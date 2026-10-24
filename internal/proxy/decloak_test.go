package proxy_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestStripCloakSuffix(t *testing.T) {
	offered := map[string]bool{
		"read":       true,
		"write":      true,
		"custom_ide": true,
		"custom_cc":  true,
	}

	tests := []struct {
		name     string
		input    string
		offered  map[string]bool
		expected string
	}{
		// Single cloaking
		{"single ide", "read_ide", offered, "read"},
		{"single cc", "read_cc", offered, "read"},
		// Double cloaking
		{"double ide", "read_ide_ide", offered, "read"},
		{"double cc", "read_cc_cc", offered, "read"},
		{"mixed ide cc", "read_ide_cc", offered, "read"},
		{"mixed cc ide", "read_cc_ide", offered, "read"},
		// Legitimate tool with suffix declared in tools
		{"legitimate ide", "custom_ide", offered, "custom_ide"},
		{"legitimate cc", "custom_cc", offered, "custom_cc"},
		{"legitimate ide cloaked once", "custom_ide_ide", offered, "custom_ide"},
		{"legitimate cc cloaked once", "custom_cc_ide", offered, "custom_cc"},
		// Tool already clean
		{"clean tool", "read", offered, "read"},
		// Tool not in offered
		{"unoffered ide", "delete_ide", offered, "delete_ide"},
		// Nil or empty offered: strips suffixes
		{"nil offered single ide", "read_ide", nil, "read"},
		{"nil offered double ide", "read_ide_ide", nil, "read"},
		{"empty offered single cc", "read_cc", map[string]bool{}, "read"},
		// Edge cases
		{"empty name", "", offered, ""},
		{"suffix only ide", "_ide", offered, "_ide"},
		{"suffix only cc", "_cc", offered, "_cc"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := proxy.StripCloakSuffix(tc.input, tc.offered)
			if got != tc.expected {
				t.Errorf("StripCloakSuffix(%q) = %q; expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestExtractOfferedTools(t *testing.T) {
	t.Run("Anthropic messages tools", func(t *testing.T) {
		body := []byte(`{
			"model": "claude-3-5-sonnet-20241022",
			"tools": [
				{"name": "read", "description": "Read file"},
				{"name": "write", "description": "Write file"},
				{"name": "custom_ide", "description": "Custom IDE tool"}
			]
		}`)
		tools := proxy.ExtractOfferedTools("messages", body)
		if len(tools) != 3 {
			t.Fatalf("expected 3 tools, got %d", len(tools))
		}
		if !tools["read"] || !tools["write"] || !tools["custom_ide"] {
			t.Errorf("unexpected extracted tools: %+v", tools)
		}
	})

	t.Run("OpenAI chat tools and functions", func(t *testing.T) {
		body := []byte(`{
			"model": "gpt-4o",
			"tools": [
				{
					"type": "function",
					"function": {"name": "read", "description": "Read file"}
				},
				{
					"type": "function",
					"function": {"name": "custom_ide", "description": "Custom"}
				}
			],
			"functions": [
				{"name": "legacy_fn", "description": "Legacy function"}
			]
		}`)
		tools := proxy.ExtractOfferedTools("chat", body)
		if len(tools) != 3 {
			t.Fatalf("expected 3 tools, got %d", len(tools))
		}
		if !tools["read"] || !tools["custom_ide"] || !tools["legacy_fn"] {
			t.Errorf("unexpected extracted tools: %+v", tools)
		}
	})

	t.Run("Empty or invalid body", func(t *testing.T) {
		if tools := proxy.ExtractOfferedTools("chat", nil); len(tools) != 0 {
			t.Errorf("expected 0 tools for nil body, got %d", len(tools))
		}
		if tools := proxy.ExtractOfferedTools("messages", []byte(`not json`)); len(tools) != 0 {
			t.Errorf("expected 0 tools for invalid JSON, got %d", len(tools))
		}
		if tools := proxy.ExtractOfferedTools("chat", []byte(`{}`)); len(tools) != 0 {
			t.Errorf("expected 0 tools for empty object, got %d", len(tools))
		}
	})
}

func TestCleanRequestToolHistory(t *testing.T) {
	offered := map[string]bool{
		"read":       true,
		"write":      true,
		"custom_ide": true,
	}

	t.Run("Anthropic messages tool history cleaning", func(t *testing.T) {
		req := `{
			"model": "claude-3-5-sonnet",
			"messages": [
				{"role": "user", "content": "Read a file"},
				{
					"role": "assistant",
					"content": [
						{"type": "text", "text": "Sure"},
						{"type": "tool_use", "id": "toolu_1", "name": "read_ide", "input": {"path": "a.go"}},
						{"type": "tool_use", "id": "toolu_2", "name": "write_ide_ide", "input": {"path": "b.go"}},
						{"type": "tool_use", "id": "toolu_3", "name": "custom_ide", "input": {}}
					]
				},
				{
					"role": "user",
					"content": [
						{"type": "tool_result", "tool_use_id": "toolu_1", "content": "package a"}
					]
				}
			],
			"tool_choice": {"type": "tool", "name": "read_ide"}
		}`

		cleaned := proxy.CleanRequestToolHistory("messages", []byte(req), offered)

		var parsed map[string]interface{}
		if err := json.Unmarshal(cleaned, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		msgs := parsed["messages"].([]interface{})
		asst := msgs[1].(map[string]interface{})
		contentList := asst["content"].([]interface{})
		block1 := contentList[1].(map[string]interface{})
		block2 := contentList[2].(map[string]interface{})
		block3 := contentList[3].(map[string]interface{})

		if block1["name"] != "read" {
			t.Errorf("expected read, got %v", block1["name"])
		}
		if block2["name"] != "write" {
			t.Errorf("expected write, got %v", block2["name"])
		}
		if block3["name"] != "custom_ide" {
			t.Errorf("expected custom_ide, got %v", block3["name"])
		}
		tc := parsed["tool_choice"].(map[string]interface{})
		if tc["name"] != "read" {
			t.Errorf("expected tool_choice read, got %v", tc["name"])
		}
	})

	t.Run("OpenAI chat tool history cleaning", func(t *testing.T) {
		req := `{
			"model": "gpt-4o",
			"messages": [
				{"role": "user", "content": "Read file"},
				{
					"role": "assistant",
					"tool_calls": [
						{
							"id": "call_1",
							"type": "function",
							"function": {"name": "read_ide", "arguments": "{}"}
						},
						{
							"id": "call_2",
							"type": "function",
							"function": {"name": "read_ide_ide", "arguments": "{}"}
						},
						{
							"id": "call_3",
							"type": "function",
							"function": {"name": "custom_ide", "arguments": "{}"}
						}
					]
				},
				{"role": "tool", "tool_call_id": "call_1", "content": "contents"}
			],
			"tool_choice": {"type": "function", "function": {"name": "read_ide"}}
		}`

		cleaned := proxy.CleanRequestToolHistory("chat", []byte(req), offered)

		var parsed struct {
			Messages []struct {
				Role      string `json:"role"`
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
			ToolChoice struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_choice"`
		}
		if err := json.Unmarshal(cleaned, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		tcs := parsed.Messages[1].ToolCalls
		if tcs[0].Function.Name != "read" {
			t.Errorf("call_1: expected read, got %s", tcs[0].Function.Name)
		}
		if tcs[1].Function.Name != "read" {
			t.Errorf("call_2: expected read, got %s", tcs[1].Function.Name)
		}
		if tcs[2].Function.Name != "custom_ide" {
			t.Errorf("call_3: expected custom_ide, got %s", tcs[2].Function.Name)
		}
		if parsed.ToolChoice.Function.Name != "read" {
			t.Errorf("tool_choice: expected read, got %s", parsed.ToolChoice.Function.Name)
		}
	})

	t.Run("Request unchanged returns original bytes pointer/slice", func(t *testing.T) {
		req := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
		cleaned := proxy.CleanRequestToolHistory("chat", req, offered)
		if &cleaned[0] != &req[0] {
			t.Errorf("expected identical byte slice when no change was made")
		}
	})
}

func TestDecloakSSEEvent(t *testing.T) {
	offered := map[string]bool{
		"read":       true,
		"custom_ide": true,
	}

	t.Run("Anthropic content_block_start single cloaked", func(t *testing.T) {
		raw := []byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"read_ide\",\"input\":{}}}\n\n")
		dataPayload := []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"read_ide","input":{}}}`)

		modified := proxy.DecloakSSEEvent("messages", raw, dataPayload, offered)

		if !strings.HasPrefix(string(modified), "event: content_block_start\n") {
			t.Errorf("expected event line preserved, got: %s", string(modified))
		}
		if !strings.HasSuffix(string(modified), "\n\n") {
			t.Errorf("expected trailing double newline preserved, got: %s", string(modified))
		}
		if strings.Contains(string(modified), `"read_ide"`) {
			t.Errorf("read_ide should have been decloaked: %s", string(modified))
		}
		if !strings.Contains(string(modified), `"read"`) {
			t.Errorf("read should be present: %s", string(modified))
		}
	})

	t.Run("Anthropic content_block_start double cloaked", func(t *testing.T) {
		raw := []byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"read_ide_ide\",\"input\":{}}}\n\n")
		dataPayload := []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"read_ide_ide","input":{}}}`)

		modified := proxy.DecloakSSEEvent("messages", raw, dataPayload, offered)

		if strings.Contains(string(modified), `"read_ide_ide"`) || strings.Contains(string(modified), `"read_ide"`) {
			t.Errorf("read_ide_ide should have been decloaked: %s", string(modified))
		}
		if !strings.Contains(string(modified), `"read"`) {
			t.Errorf("read should be present: %s", string(modified))
		}
	})

	t.Run("Anthropic content_block_start legitimate custom_ide", func(t *testing.T) {
		raw := []byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"custom_ide\",\"input\":{}}}\n\n")
		dataPayload := []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"custom_ide","input":{}}}`)

		modified := proxy.DecloakSSEEvent("messages", raw, dataPayload, offered)
		if !bytes.Equal(modified, raw) {
			t.Errorf("legitimate custom_ide should not be modified, got: %s", string(modified))
		}
	})

	t.Run("Anthropic non-tool event untouched", func(t *testing.T) {
		raw := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
		dataPayload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)

		modified := proxy.DecloakSSEEvent("messages", raw, dataPayload, offered)
		if !bytes.Equal(modified, raw) {
			t.Errorf("text delta should not be modified, got: %s", string(modified))
		}
	})

	t.Run("OpenAI chat SSE chunk single cloaked", func(t *testing.T) {
		raw := []byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_ide\",\"arguments\":\"\"}}]}}]}\n\n")
		dataPayload := []byte(`{"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_ide","arguments":""}}]}}]}`)

		modified := proxy.DecloakSSEEvent("chat", raw, dataPayload, offered)

		if strings.Contains(string(modified), `"read_ide"`) {
			t.Errorf("read_ide should have been decloaked: %s", string(modified))
		}
		if !strings.Contains(string(modified), `"read"`) {
			t.Errorf("read should be present: %s", string(modified))
		}
		if !strings.HasSuffix(string(modified), "\n\n") {
			t.Errorf("expected trailing double newline preserved: %s", string(modified))
		}
	})

	t.Run("OpenAI chat SSE chunk double cloaked with CRLF", func(t *testing.T) {
		raw := []byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_ide_ide\",\"arguments\":\"\"}}]}}]}\r\n\r\n")
		dataPayload := []byte(`{"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_ide_ide","arguments":""}}]}}]}`)

		modified := proxy.DecloakSSEEvent("chat", raw, dataPayload, offered)

		if strings.Contains(string(modified), `"read_ide_ide"`) {
			t.Errorf("read_ide_ide should have been decloaked: %s", string(modified))
		}
		if !strings.Contains(string(modified), `"read"`) {
			t.Errorf("read should be present: %s", string(modified))
		}
		if !bytes.HasSuffix(modified, []byte("\r\n\r\n")) {
			t.Errorf("expected CRLF line ending preserved")
		}
	})

	t.Run("OpenAI chat DONE event untouched", func(t *testing.T) {
		raw := []byte("data: [DONE]\n\n")
		modified := proxy.DecloakSSEEvent("chat", raw, []byte("[DONE]"), offered)
		if !bytes.Equal(modified, raw) {
			t.Errorf("[DONE] should remain untouched")
		}
	})
}

func TestDecloakResponseBody(t *testing.T) {
	offered := map[string]bool{
		"read":       true,
		"custom_ide": true,
	}

	t.Run("Anthropic non-streaming response", func(t *testing.T) {
		body := []byte(`{
			"id": "msg_01",
			"type": "message",
			"role": "assistant",
			"content": [
				{"type": "text", "text": "I will read the file."},
				{"type": "tool_use", "id": "toolu_1", "name": "read_ide_ide", "input": {"path": "main.go"}},
				{"type": "tool_use", "id": "toolu_2", "name": "custom_ide", "input": {}}
			]
		}`)

		decloaked := proxy.DecloakResponseBody("messages", body, offered)

		var parsed struct {
			Content []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content"`
		}
		if err := json.Unmarshal(decloaked, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if parsed.Content[1].Name != "read" {
			t.Errorf("expected read, got %s", parsed.Content[1].Name)
		}
		if parsed.Content[2].Name != "custom_ide" {
			t.Errorf("expected custom_ide, got %s", parsed.Content[2].Name)
		}
	})

	t.Run("OpenAI non-streaming response", func(t *testing.T) {
		body := []byte(`{
			"id": "chatcmpl-1",
			"choices": [
				{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": null,
						"tool_calls": [
							{
								"id": "call_1",
								"type": "function",
								"function": {"name": "read_ide", "arguments": "{}"}
							},
							{
								"id": "call_2",
								"type": "function",
								"function": {"name": "custom_ide", "arguments": "{}"}
							}
						]
					}
				}
			]
		}`)

		decloaked := proxy.DecloakResponseBody("chat", body, offered)

		var parsed struct {
			Choices []struct {
				Message struct {
					ToolCalls []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(decloaked, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		tcs := parsed.Choices[0].Message.ToolCalls
		if tcs[0].Function.Name != "read" {
			t.Errorf("call_1: expected read, got %s", tcs[0].Function.Name)
		}
		if tcs[1].Function.Name != "custom_ide" {
			t.Errorf("call_2: expected custom_ide, got %s", tcs[1].Function.Name)
		}
	})

	t.Run("Response without tool calls returns original bytes", func(t *testing.T) {
		body := []byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"hello"}}]}`)
		decloaked := proxy.DecloakResponseBody("chat", body, offered)
		if &decloaked[0] != &body[0] {
			t.Errorf("expected identical byte slice when no change was made")
		}
	})
}

func TestPreserveThinkingIntent(t *testing.T) {
	t.Run("chat with thinking object injects medium effort", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled"}}`)
		out := proxy.PreserveThinkingIntent(body)

		var parsed map[string]interface{}
		_ = json.Unmarshal(out, &parsed)
		if parsed["reasoning_effort"] != "medium" {
			t.Errorf("expected reasoning_effort 'medium', got %v", parsed["reasoning_effort"])
		}
	})

	t.Run("chat with thinking high budget injects high effort", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","thinking":{"type":"enabled","budget_tokens":16000}}`)
		out := proxy.PreserveThinkingIntent(body)

		var parsed map[string]interface{}
		_ = json.Unmarshal(out, &parsed)
		if parsed["reasoning_effort"] != "high" {
			t.Errorf("expected reasoning_effort 'high', got %v", parsed["reasoning_effort"])
		}
	})

	t.Run("chat with thinking low budget injects low effort", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","thinking":{"type":"enabled","budget_tokens":1000}}`)
		out := proxy.PreserveThinkingIntent(body)

		var parsed map[string]interface{}
		_ = json.Unmarshal(out, &parsed)
		if parsed["reasoning_effort"] != "low" {
			t.Errorf("expected reasoning_effort 'low', got %v", parsed["reasoning_effort"])
		}
	})

	t.Run("chat with thinking explicit effort injects specified effort", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","thinking":{"type":"enabled","effort":"high"}}`)
		out := proxy.PreserveThinkingIntent(body)

		var parsed map[string]interface{}
		_ = json.Unmarshal(out, &parsed)
		if parsed["reasoning_effort"] != "high" {
			t.Errorf("expected reasoning_effort 'high', got %v", parsed["reasoning_effort"])
		}
	})

	t.Run("chat with pre-existing reasoning_effort is preserved", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","thinking":{"type":"enabled"},"reasoning_effort":"low"}`)
		out := proxy.PreserveThinkingIntent(body)

		var parsed map[string]interface{}
		_ = json.Unmarshal(out, &parsed)
		if parsed["reasoning_effort"] != "low" {
			t.Errorf("expected existing reasoning_effort 'low' preserved, got %v", parsed["reasoning_effort"])
		}
	})

	t.Run("chat without thinking object is untouched", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
		out := proxy.PreserveThinkingIntent(body)

		var parsed map[string]interface{}
		_ = json.Unmarshal(out, &parsed)
		if _, ok := parsed["reasoning_effort"]; ok {
			t.Errorf("reasoning_effort should not be injected when thinking is absent")
		}
	})

	t.Run("chat with thinking disabled is untouched", func(t *testing.T) {
		body := []byte(`{"model":"gpt-4o","thinking":{"type":"disabled"}}`)
		out := proxy.PreserveThinkingIntent(body)

		var parsed map[string]interface{}
		_ = json.Unmarshal(out, &parsed)
		if _, ok := parsed["reasoning_effort"]; ok {
			t.Errorf("reasoning_effort should not be injected when thinking is disabled")
		}
	})
}

func TestProxyDecloakAndThinkingIntegration(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	var lastUpstreamBody []byte

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastUpstreamBody = body

		if strings.HasSuffix(r.URL.Path, "/messages") {
			if r.URL.Query().Get("stream") == "true" || strings.Contains(string(body), `"stream":true`) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_99\",\"name\":\"read_ide\",\"input\":{}}}\n\n"))
				_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"content": [
					{"type": "tool_use", "id": "toolu_1", "name": "read_ide", "input": {}}
				],
				"usage": {"input_tokens": 100, "output_tokens": 50}
			}`))
			return
		}

		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			if strings.Contains(string(body), `"stream":true`) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"read_ide\",\"arguments\":\"\"}}]}}]}\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "chatcmpl-123",
				"choices": [
					{
						"message": {
							"role": "assistant",
							"tool_calls": [
								{"id": "call_1", "type": "function", "function": {"name": "read_ide", "arguments": "{}"}}
							]
						}
					}
				],
				"usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	_, err = providersMgr.CreateProvider("Mock Provider", mockUpstream.URL, "mock-upstream-key", "mock", true, true)
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	clientKey, err := keyMgr.CreateKey("Test Client", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	// 1. Anthropic POST /v1/messages non-streaming:
	// - Cleans assistant history (read_ide -> read)
	// - Upstream receives de-poisoned history
	// - Upstream responds with read_ide -> client receives decloaked read
	// - Traffic log message uses POST /v1/messages
	t.Run("Anthropic messages non-stream decloak and traffic log", func(t *testing.T) {
		msgReq := `{
			"model": "mock/claude-3-7-sonnet",
			"messages": [
				{"role": "user", "content": "read it"},
				{
					"role": "assistant",
					"content": [
						{"type": "tool_use", "id": "t1", "name": "read_ide", "input": {}}
					]
				},
				{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "t1", "content": "data"}]}
			],
			"tools": [
				{"name": "read", "description": "read"}
			]
		}`
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(msgReq))
		r.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
		w := httptest.NewRecorder()

		p.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		// Verify upstream received de-poisoned history
		if strings.Contains(string(lastUpstreamBody), `"read_ide"`) {
			t.Errorf("upstream should have received cleaned history without read_ide, got: %s", string(lastUpstreamBody))
		}

		// Verify client received decloaked response
		var clientResp struct {
			Content []struct {
				Name string `json:"name"`
			} `json:"content"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &clientResp); err != nil {
			t.Fatalf("unmarshal client response: %v", err)
		}
		if len(clientResp.Content) == 0 || clientResp.Content[0].Name != "read" {
			t.Errorf("expected client response to have tool name 'read', got: %+v", clientResp)
		}

		// Verify traffic log message
		time.Sleep(50 * time.Millisecond)
		logs, _, err := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 5})
		if err != nil || len(logs) == 0 {
			t.Fatalf("failed to query traffic logs")
		}
		if !strings.HasPrefix(logs[0].Message, "POST /v1/messages model=") {
			t.Errorf("expected traffic log message to start with 'POST /v1/messages model=', got: %q", logs[0].Message)
		}
		if logs[0].ApiFormat != "messages" {
			t.Errorf("expected ApiFormat 'messages', got: %q", logs[0].ApiFormat)
		}
	})

	// 2. Anthropic POST /v1/messages streaming:
	// Upstream emits read_ide, client stream receives read
	t.Run("Anthropic messages SSE stream decloak", func(t *testing.T) {
		msgReq := `{
			"model": "mock/claude-3-7-sonnet",
			"stream": true,
			"messages": [
				{"role": "user", "content": "read it"}
			],
			"tools": [
				{"name": "read", "description": "read"}
			]
		}`
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(msgReq))
		r.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
		w := httptest.NewRecorder()

		p.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		respStr := w.Body.String()
		if strings.Contains(respStr, `"read_ide"`) {
			t.Errorf("SSE stream should not contain read_ide: %s", respStr)
		}
		if !strings.Contains(respStr, `"read"`) {
			t.Errorf("SSE stream should contain decloaked read: %s", respStr)
		}
	})

	// 3. OpenAI POST /v1/chat/completions:
	// - thinking object present -> injects reasoning_effort: "medium"
	// - de-poisons double-cloaked tool history (read_ide_ide -> read)
	// - decloaks non-streaming response tool_calls
	t.Run("OpenAI chat completions thinking intent preservation & decloaking", func(t *testing.T) {
		chatReq := `{
			"model": "mock/gpt-4o",
			"messages": [
				{"role": "user", "content": "run task"},
				{
					"role": "assistant",
					"tool_calls": [
						{"id": "c1", "type": "function", "function": {"name": "read_ide_ide", "arguments": "{}"}}
					]
				},
				{"role": "tool", "tool_call_id": "c1", "content": "ok"}
			],
			"tools": [
				{"type": "function", "function": {"name": "read"}}
			],
			"thinking": {
				"type": "enabled"
			}
		}`
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReq))
		r.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
		w := httptest.NewRecorder()

		p.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		// Verify reasoning_effort was injected into forward request to upstream
		var forwardParsed map[string]interface{}
		if err := json.Unmarshal(lastUpstreamBody, &forwardParsed); err != nil {
			t.Fatalf("unmarshal upstream body: %v", err)
		}
		if forwardParsed["reasoning_effort"] != "medium" {
			t.Errorf("expected reasoning_effort 'medium' injected for upstream, got: %v", forwardParsed["reasoning_effort"])
		}

		// Verify history de-poisoning for upstream
		if strings.Contains(string(lastUpstreamBody), `"read_ide_ide"`) {
			t.Errorf("upstream should not receive read_ide_ide: %s", string(lastUpstreamBody))
		}

		// Verify client received decloaked response
		var clientResp struct {
			Choices []struct {
				Message struct {
					ToolCalls []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &clientResp); err != nil {
			t.Fatalf("unmarshal client response: %v", err)
		}
		if clientResp.Choices[0].Message.ToolCalls[0].Function.Name != "read" {
			t.Errorf("expected decloaked name 'read', got: %s", clientResp.Choices[0].Message.ToolCalls[0].Function.Name)
		}
	})

	// 4. OpenAI POST /v1/chat/completions streaming decloak:
	t.Run("OpenAI chat completions SSE stream decloak", func(t *testing.T) {
		chatReq := `{
			"model": "mock/gpt-4o",
			"stream": true,
			"messages": [
				{"role": "user", "content": "read"}
			],
			"tools": [
				{"type": "function", "function": {"name": "read"}}
			]
		}`
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReq))
		r.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
		w := httptest.NewRecorder()

		p.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		respStr := w.Body.String()
		if strings.Contains(respStr, `"read_ide"`) {
			t.Errorf("SSE stream should not contain read_ide: %s", respStr)
		}
		if !strings.Contains(respStr, `"read"`) {
			t.Errorf("SSE stream should contain decloaked read: %s", respStr)
		}
	})
}
