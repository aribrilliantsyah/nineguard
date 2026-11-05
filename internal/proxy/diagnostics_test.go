package proxy_test

import (
	"encoding/json"
	"testing"

	"nineguard/internal/proxy"
)

func TestDetectAPIFormat(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/v1/chat/completions", "chat"},
		{"/chat/completions", "chat"},
		{"/v1beta/chat/completions", "chat"},
		{"/v1/chat/completions/", "chat"},
		{"/v1/messages", "messages"},
		{"/messages", "messages"},
		{"/v1beta/messages", "messages"},
		{"/v1/responses", "responses"},
		{"/responses", "responses"},
		{"/v1/embeddings", "other"},
		{"/v1/models", "other"},
		{"/other/path", "other"},
	}

	for _, tc := range tests {
		actual := proxy.DetectAPIFormat(tc.path)
		if actual != tc.expected {
			t.Errorf("DetectAPIFormat(%q) = %q; expected %q", tc.path, actual, tc.expected)
		}
	}
}

func TestDetectThinkingRequested(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		body      string
		modelName string
		expected  bool
	}{
		{
			name:     "chat with reasoning_effort medium",
			format:   "chat",
			body:     `{"model":"gpt-4o","reasoning_effort":"medium"}`,
			expected: true,
		},
		{
			name:     "chat with reasoning_effort none",
			format:   "chat",
			body:     `{"model":"gpt-4o","reasoning_effort":"none"}`,
			expected: false,
		},
		{
			name:     "chat with reasoning object enabled",
			format:   "chat",
			body:     `{"model":"claude-3.7-sonnet","reasoning":{"enabled":true}}`,
			expected: true,
		},
		{
			name:     "chat with reasoning object effort high",
			format:   "chat",
			body:     `{"model":"claude-3.7-sonnet","reasoning":{"effort":"high"}}`,
			expected: true,
		},
		{
			name:     "chat with reasoning object max_tokens",
			format:   "chat",
			body:     `{"model":"claude-3.7-sonnet","reasoning":{"max_tokens":4096}}`,
			expected: true,
		},
		{
			name:     "chat with thinking object enabled",
			format:   "chat",
			body:     `{"model":"claude-3.7-sonnet","thinking":{"type":"enabled"}}`,
			expected: true,
		},
		{
			name:     "chat with thinking object adaptive",
			format:   "chat",
			body:     `{"model":"claude-3.7-sonnet","thinking":{"type":"adaptive"}}`,
			expected: true,
		},
		{
			name:     "messages with thinking enabled",
			format:   "messages",
			body:     `{"model":"claude-3.7-sonnet","thinking":{"type":"enabled","budget_tokens":2048}}`,
			expected: true,
		},
		{
			name:     "responses with reasoning effort",
			format:   "responses",
			body:     `{"model":"o1","reasoning":{"effort":"high"}}`,
			expected: true,
		},
		{
			name:      "model name has thinking tag",
			format:    "chat",
			body:      `{"model":"openrouter/anthropic/claude-3.7-sonnet:thinking"}`,
			modelName: "openrouter/anthropic/claude-3.7-sonnet:thinking",
			expected:  true,
		},
		{
			name:      "standard request without thinking",
			format:    "chat",
			body:      `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`,
			modelName: "gpt-4o",
			expected:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := proxy.DetectThinkingRequested(tc.format, []byte(tc.body), tc.modelName)
			if actual != tc.expected {
				t.Errorf("DetectThinkingRequested() = %v; expected %v", actual, tc.expected)
			}
		})
	}
}

func TestParseDiagnosticsNonStream(t *testing.T) {
	t.Run("OpenAI chat with cache and reasoning details", func(t *testing.T) {
		body := `{
			"id": "chatcmpl-123",
			"model": "gpt-4o-2024-08-06",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hello",
					"reasoning_content": "Thought process here..."
				}
			}],
			"usage": {
				"prompt_tokens": 1200,
				"completion_tokens": 300,
				"total_tokens": 1500,
				"prompt_tokens_details": {
					"cached_tokens": 1000,
					"cache_write_tokens": 50
				},
				"completion_tokens_details": {
					"reasoning_tokens": 200
				}
			}
		}`

		diag := proxy.ParseNonStreamDiagnostics("chat", []byte(body))
		if diag.ServedModel != "gpt-4o-2024-08-06" {
			t.Errorf("ServedModel = %q; expected 'gpt-4o-2024-08-06'", diag.ServedModel)
		}
		if diag.PromptTokens != 1200 || diag.CompletionTokens != 300 || diag.TotalTokens != 1500 {
			t.Errorf("Tokens = %d/%d/%d; expected 1200/300/1500", diag.PromptTokens, diag.CompletionTokens, diag.TotalTokens)
		}
		if diag.CacheReadTokens != 1000 {
			t.Errorf("CacheReadTokens = %d; expected 1000", diag.CacheReadTokens)
		}
		if diag.CacheWriteTokens != 50 {
			t.Errorf("CacheWriteTokens = %d; expected 50", diag.CacheWriteTokens)
		}
		if diag.ReasoningTokens != 200 {
			t.Errorf("ReasoningTokens = %d; expected 200", diag.ReasoningTokens)
		}
		if !diag.ThinkingDelivered {
			t.Errorf("expected ThinkingDelivered=true")
		}
	})

	t.Run("Anthropic messages with thinking blocks and caching", func(t *testing.T) {
		body := `{
			"id": "msg_123",
			"model": "claude-3-7-sonnet-20250219",
			"type": "message",
			"role": "assistant",
			"content": [
				{
					"type": "thinking",
					"thinking": "Pondering the prompt..."
				},
				{
					"type": "text",
					"text": "Final answer."
				}
			],
			"usage": {
				"input_tokens": 200,
				"cache_creation_input_tokens": 150,
				"cache_read_input_tokens": 800,
				"output_tokens": 400
			}
		}`

		diag := proxy.ParseNonStreamDiagnostics("messages", []byte(body))
		if diag.ServedModel != "claude-3-7-sonnet-20250219" {
			t.Errorf("ServedModel = %q; expected 'claude-3-7-sonnet-20250219'", diag.ServedModel)
		}
		// Normalized prompt = 200 + 150 + 800 = 1150
		if diag.PromptTokens != 1150 {
			t.Errorf("PromptTokens = %d; expected 1150", diag.PromptTokens)
		}
		if diag.CompletionTokens != 400 {
			t.Errorf("CompletionTokens = %d; expected 400", diag.CompletionTokens)
		}
		if diag.TotalTokens != 1550 {
			t.Errorf("TotalTokens = %d; expected 1550", diag.TotalTokens)
		}
		if diag.CacheReadTokens != 800 {
			t.Errorf("CacheReadTokens = %d; expected 800", diag.CacheReadTokens)
		}
		if diag.CacheWriteTokens != 150 {
			t.Errorf("CacheWriteTokens = %d; expected 150", diag.CacheWriteTokens)
		}
		if !diag.ThinkingDelivered {
			t.Errorf("expected ThinkingDelivered=true for thinking block")
		}
	})

	t.Run("Responses format with reasoning items", func(t *testing.T) {
		body := `{
			"id": "resp_123",
			"model": "o3-mini",
			"output": [
				{
					"type": "reasoning",
					"summary": "Thinking step"
				},
				{
					"type": "message",
					"content": "Result"
				}
			],
			"usage": {
				"input_tokens": 500,
				"output_tokens": 250,
				"total_tokens": 750,
				"input_tokens_details": {
					"cached_tokens": 300
				},
				"output_tokens_details": {
					"reasoning_tokens": 150
				}
			}
		}`

		diag := proxy.ParseNonStreamDiagnostics("responses", []byte(body))
		if diag.ServedModel != "o3-mini" {
			t.Errorf("ServedModel = %q; expected 'o3-mini'", diag.ServedModel)
		}
		if diag.PromptTokens != 500 || diag.CompletionTokens != 250 || diag.TotalTokens != 750 {
			t.Errorf("Tokens = %d/%d/%d; expected 500/250/750", diag.PromptTokens, diag.CompletionTokens, diag.TotalTokens)
		}
		if diag.CacheReadTokens != 300 {
			t.Errorf("CacheReadTokens = %d; expected 300", diag.CacheReadTokens)
		}
		if diag.ReasoningTokens != 150 {
			t.Errorf("ReasoningTokens = %d; expected 150", diag.ReasoningTokens)
		}
		if !diag.ThinkingDelivered {
			t.Errorf("expected ThinkingDelivered=true for reasoning output")
		}
	})
}

func TestParseDiagnosticsStream(t *testing.T) {
	t.Run("OpenAI chat stream with reasoning delta and usage chunk", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("chat")

		// Chunk 1: model & role
		parser.FeedSSEEvent([]byte("data: {\"id\":\"1\",\"model\":\"gpt-4o\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"),
			[]byte(`{"id":"1","model":"gpt-4o","choices":[{"delta":{"role":"assistant"}}]}`))

		// Chunk 2: reasoning content delta
		parser.FeedSSEEvent([]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking step...\"}}]}\n\n"),
			[]byte(`{"id":"1","choices":[{"delta":{"reasoning_content":"thinking step..."}}]}`))

		// Chunk 3: text delta
		parser.FeedSSEEvent([]byte("data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hello!\"}}]}\n\n"),
			[]byte(`{"id":"1","choices":[{"delta":{"content":"Hello!"}}]}`))

		// Chunk 4: usage-only chunk
		parser.FeedSSEEvent([]byte("data: {\"id\":\"1\",\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":150,\"total_tokens\":1150,\"prompt_tokens_details\":{\"cached_tokens\":800},\"completion_tokens_details\":{\"reasoning_tokens\":100}}}\n\n"),
			[]byte(`{"id":"1","choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":150,"total_tokens":1150,"prompt_tokens_details":{"cached_tokens":800},"completion_tokens_details":{"reasoning_tokens":100}}}`))

		diag := parser.Finalize(4, 200, 200)
		if diag.ServedModel != "gpt-4o" {
			t.Errorf("ServedModel = %q; expected 'gpt-4o'", diag.ServedModel)
		}
		if diag.PromptTokens != 1000 || diag.CompletionTokens != 150 || diag.TotalTokens != 1150 {
			t.Errorf("Tokens = %d/%d/%d; expected 1000/150/1150", diag.PromptTokens, diag.CompletionTokens, diag.TotalTokens)
		}
		if diag.CacheReadTokens != 800 {
			t.Errorf("CacheReadTokens = %d; expected 800", diag.CacheReadTokens)
		}
		if diag.ReasoningTokens != 100 {
			t.Errorf("ReasoningTokens = %d; expected 100", diag.ReasoningTokens)
		}
		if !diag.ThinkingDelivered {
			t.Errorf("expected ThinkingDelivered=true")
		}
		if diag.UsageEstimated {
			t.Errorf("expected UsageEstimated=false")
		}
	})

	t.Run("Anthropic messages stream with thinking block and message delta", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("messages")

		// Event 1: message_start with model and prompt usage
		parser.FeedSSEEvent([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-3-7-sonnet-20250219\",\"usage\":{\"input_tokens\":300,\"cache_read_input_tokens\":1200,\"cache_creation_input_tokens\":100}}}\n\n"),
			[]byte(`{"type":"message_start","message":{"model":"claude-3-7-sonnet-20250219","usage":{"input_tokens":300,"cache_read_input_tokens":1200,"cache_creation_input_tokens":100}}}`))

		// Event 2: content_block_start thinking
		parser.FeedSSEEvent([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n"),
			[]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`))

		// Event 3: message_delta with output tokens
		parser.FeedSSEEvent([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":250}}\n\n"),
			[]byte(`{"type":"message_delta","usage":{"output_tokens":250}}`))

		diag := parser.Finalize(3, 400, 200)
		if diag.ServedModel != "claude-3-7-sonnet-20250219" {
			t.Errorf("ServedModel = %q; expected 'claude-3-7-sonnet-20250219'", diag.ServedModel)
		}
		// Prompt = 300 + 1200 + 100 = 1600
		if diag.PromptTokens != 1600 {
			t.Errorf("PromptTokens = %d; expected 1600", diag.PromptTokens)
		}
		if diag.CompletionTokens != 250 {
			t.Errorf("CompletionTokens = %d; expected 250", diag.CompletionTokens)
		}
		if diag.TotalTokens != 1850 {
			t.Errorf("TotalTokens = %d; expected 1850", diag.TotalTokens)
		}
		if diag.CacheReadTokens != 1200 {
			t.Errorf("CacheReadTokens = %d; expected 1200", diag.CacheReadTokens)
		}
		if diag.CacheWriteTokens != 100 {
			t.Errorf("CacheWriteTokens = %d; expected 100", diag.CacheWriteTokens)
		}
		if !diag.ThinkingDelivered {
			t.Errorf("expected ThinkingDelivered=true for thinking content_block_start")
		}
	})

	t.Run("OpenAI chat stream with prompt and completion tokens but zero total_tokens", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("chat")
		// Usage chunk has prompt_tokens and completion_tokens, but total_tokens is 0
		parser.FeedSSEEvent([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":80,\"total_tokens\":0}}\n\n"),
			[]byte(`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":80,"total_tokens":0}}`))

		diag := parser.Finalize(1, 200, 200)
		if diag.PromptTokens != 120 {
			t.Errorf("PromptTokens = %d, expected 120", diag.PromptTokens)
		}
		if diag.CompletionTokens != 80 {
			t.Errorf("CompletionTokens = %d, expected 80", diag.CompletionTokens)
		}
		if diag.TotalTokens != 200 {
			t.Errorf("TotalTokens = %d, expected 200 (prompt+completion)", diag.TotalTokens)
		}
		if diag.UsageEstimated {
			t.Errorf("UsageEstimated = true, expected false")
		}
	})

	t.Run("Anthropic messages stream with input/cache only on message_delta", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("messages")

		// message_start with all 0 usage
		parser.FeedSSEEvent([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-3-7-sonnet-20250219\",\"usage\":{\"input_tokens\":0,\"cache_read_input_tokens\":0,\"cache_creation_input_tokens\":0}}}\n\n"),
			[]byte(`{"type":"message_start","message":{"model":"claude-3-7-sonnet-20250219","usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`))

		// message_delta with input_tokens, cache_read, cache_creation, output_tokens
		parser.FeedSSEEvent([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":400,\"cache_read_input_tokens\":1000,\"cache_creation_input_tokens\":50,\"output_tokens\":200}}\n\n"),
			[]byte(`{"type":"message_delta","usage":{"input_tokens":400,"cache_read_input_tokens":1000,"cache_creation_input_tokens":50,"output_tokens":200}}`))

		diag := parser.Finalize(2, 400, 200)
		// Prompt = 400 + 1000 + 50 = 1450
		if diag.PromptTokens != 1450 {
			t.Errorf("PromptTokens = %d; expected 1450", diag.PromptTokens)
		}
		if diag.CompletionTokens != 200 {
			t.Errorf("CompletionTokens = %d; expected 200", diag.CompletionTokens)
		}
		if diag.TotalTokens != 1650 {
			t.Errorf("TotalTokens = %d; expected 1650", diag.TotalTokens)
		}
		if diag.CacheReadTokens != 1000 {
			t.Errorf("CacheReadTokens = %d; expected 1000", diag.CacheReadTokens)
		}
		if diag.CacheWriteTokens != 50 {
			t.Errorf("CacheWriteTokens = %d; expected 50", diag.CacheWriteTokens)
		}
		if diag.UsageEstimated {
			t.Errorf("UsageEstimated = true; expected false")
		}
	})

	t.Run("Anthropic messages stream keeps message_start values when present", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("messages")

		// message_start with non-zero usage
		parser.FeedSSEEvent([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-3-7-sonnet-20250219\",\"usage\":{\"input_tokens\":300,\"cache_read_input_tokens\":1200,\"cache_creation_input_tokens\":100}}}\n\n"),
			[]byte(`{"type":"message_start","message":{"model":"claude-3-7-sonnet-20250219","usage":{"input_tokens":300,"cache_read_input_tokens":1200,"cache_creation_input_tokens":100}}}`))

		// message_delta with different input/cache values that should NOT overwrite message_start
		parser.FeedSSEEvent([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":999,\"cache_read_input_tokens\":888,\"cache_creation_input_tokens\":777,\"output_tokens\":250}}\n\n"),
			[]byte(`{"type":"message_delta","usage":{"input_tokens":999,"cache_read_input_tokens":888,"cache_creation_input_tokens":777,"output_tokens":250}}`))

		diag := parser.Finalize(2, 400, 200)
		// Prompt = 300 + 1200 + 100 = 1600 (not 999+888+777)
		if diag.PromptTokens != 1600 {
			t.Errorf("PromptTokens = %d; expected 1600", diag.PromptTokens)
		}
		if diag.CompletionTokens != 250 {
			t.Errorf("CompletionTokens = %d; expected 250", diag.CompletionTokens)
		}
		if diag.TotalTokens != 1850 {
			t.Errorf("TotalTokens = %d; expected 1850", diag.TotalTokens)
		}
		if diag.CacheReadTokens != 1200 {
			t.Errorf("CacheReadTokens = %d; expected 1200", diag.CacheReadTokens)
		}
		if diag.CacheWriteTokens != 100 {
			t.Errorf("CacheWriteTokens = %d; expected 100", diag.CacheWriteTokens)
		}
	})

	t.Run("Stream with missing usage triggers estimated flag", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("chat")
		parser.FeedSSEEvent([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"),
			[]byte(`{"choices":[{"delta":{"content":"a"}}]}`))

		diag := parser.Finalize(10, 400, 200)
		if !diag.UsageEstimated {
			t.Errorf("expected UsageEstimated=true when no usage reported")
		}
		// Prompt = 400/4 = 100, completion = max(1, 10) = 10, total = 110
		if diag.PromptTokens != 100 || diag.CompletionTokens != 10 || diag.TotalTokens != 110 {
			t.Errorf("estimated tokens = %d/%d/%d; expected 100/10/110", diag.PromptTokens, diag.CompletionTokens, diag.TotalTokens)
		}
	})
}

func TestBuildStreamSummary(t *testing.T) {
	t.Run("Anthropic messages stream summary reconstruction", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("messages")

		// 1. message_start
		parser.FeedSSEEvent(nil, []byte(`{"type":"message_start","message":{"model":"claude-3-5-sonnet","usage":{"input_tokens":100,"cache_read_input_tokens":20,"cache_creation_input_tokens":10}}}`))

		// 2. content_block_start (thinking)
		parser.FeedSSEEvent(nil, []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`))

		// 3. content_block_delta (thinking)
		parser.FeedSSEEvent(nil, []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Plan: check the weather."}}`))

		// 4. content_block_start (tool_use)
		parser.FeedSSEEvent(nil, []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather"}}`))

		// 5. content_block_delta (input_json_delta chunk 1)
		parser.FeedSSEEvent(nil, []byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`))

		// 6. content_block_delta (input_json_delta chunk 2)
		parser.FeedSSEEvent(nil, []byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Paris\"}"}}`))

		// 7. content_block_start (text)
		parser.FeedSSEEvent(nil, []byte(`{"type":"content_block_start","index":2,"content_block":{"type":"text"}}`))

		// 8. content_block_delta (text)
		parser.FeedSSEEvent(nil, []byte(`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Checking weather for Paris..."}}`))

		// 9. message_delta
		parser.FeedSSEEvent(nil, []byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":45}}`))

		diag := parser.Finalize(9, 500, 200)
		summaryBytes := parser.BuildStreamSummary(9, diag)

		var summary proxy.StreamSummary
		if err := json.Unmarshal(summaryBytes, &summary); err != nil {
			t.Fatalf("failed to unmarshal summary JSON: %v", err)
		}

		if !summary.StreamReconstructed {
			t.Errorf("expected StreamReconstructed=true")
		}
		if summary.APIFormat != "messages" {
			t.Errorf("expected APIFormat='messages', got %q", summary.APIFormat)
		}
		if summary.ServedModel != "claude-3-5-sonnet" {
			t.Errorf("expected ServedModel='claude-3-5-sonnet', got %q", summary.ServedModel)
		}
		if summary.Chunks != 9 {
			t.Errorf("expected Chunks=9, got %d", summary.Chunks)
		}
		if summary.StopReason != "tool_use" {
			t.Errorf("expected StopReason='tool_use', got %q", summary.StopReason)
		}
		if !summary.ThinkingDelivered {
			t.Errorf("expected ThinkingDelivered=true")
		}
		if summary.Thinking != "Plan: check the weather." {
			t.Errorf("expected Thinking='Plan: check the weather.', got %q", summary.Thinking)
		}
		if summary.Content != "Checking weather for Paris..." {
			t.Errorf("expected Content='Checking weather for Paris...', got %q", summary.Content)
		}
		if len(summary.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(summary.ToolCalls))
		}
		if summary.ToolCalls[0].ID != "toolu_123" || summary.ToolCalls[0].Name != "get_weather" || summary.ToolCalls[0].Arguments != `{"location":"Paris"}` {
			t.Errorf("unexpected tool call: %+v", summary.ToolCalls[0])
		}
		if summary.Usage.PromptTokens != 130 || summary.Usage.CompletionTokens != 45 || summary.Usage.TotalTokens != 175 {
			t.Errorf("unexpected usage: %+v", summary.Usage)
		}
	})

	t.Run("Responses stream with reasoning items and text delta", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("responses")

		// 1. output_item.added with item.type: "reasoning"
		parser.FeedSSEEvent(nil, []byte(`{"type":"response.output_item.added","item":{"type":"reasoning","encrypted_content":"xyz"}}`))

		// 2. reasoning_summary_text.delta
		parser.FeedSSEEvent(nil, []byte(`{"type":"response.reasoning_summary_text.delta","delta":"thinking about primes"}`))

		// 3. output_text.delta
		parser.FeedSSEEvent(nil, []byte(`{"type":"response.output_text.delta","delta":"397"}`))

		// 4. response.completed with usage output_tokens_details.reasoning_tokens = 0
		parser.FeedSSEEvent(nil, []byte(`{"type":"response.completed","response":{"model":"gpt-4o","usage":{"input_tokens":100,"output_tokens":50,"total_tokens":150,"output_tokens_details":{"reasoning_tokens":0}}}}`))

		diag := parser.Finalize(4, 200, 200)
		summaryBytes := parser.BuildStreamSummary(4, diag)

		var summary proxy.StreamSummary
		if err := json.Unmarshal(summaryBytes, &summary); err != nil {
			t.Fatalf("failed to unmarshal summary JSON: %v", err)
		}

		if !diag.ThinkingDelivered {
			t.Errorf("expected diag.ThinkingDelivered=true, got false")
		}
		if !summary.ThinkingDelivered {
			t.Errorf("expected summary.ThinkingDelivered=true, got false")
		}
		if summary.Thinking != "thinking about primes" {
			t.Errorf("expected Thinking='thinking about primes', got %q", summary.Thinking)
		}
		if summary.Content != "397" {
			t.Errorf("expected Content='397', got %q", summary.Content)
		}
	})

	t.Run("Responses stream function_call_arguments delta does not pollute content", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("responses")

		// 1. function_call_arguments.delta
		parser.FeedSSEEvent(nil, []byte(`{"type":"response.function_call_arguments.delta","delta":"{\"location\":\"Paris\"}"}`))

		// 2. output_text.delta
		parser.FeedSSEEvent(nil, []byte(`{"type":"response.output_text.delta","delta":"Here is the weather"}`))

		diag := parser.Finalize(2, 100, 200)
		summaryBytes := parser.BuildStreamSummary(2, diag)

		var summary proxy.StreamSummary
		if err := json.Unmarshal(summaryBytes, &summary); err != nil {
			t.Fatalf("failed to unmarshal summary JSON: %v", err)
		}

		if summary.Content != "Here is the weather" {
			t.Errorf("expected Content='Here is the weather', got %q", summary.Content)
		}
	})

	t.Run("OpenAI chat stream summary reconstruction", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("chat")

		// 1. Role and model
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-abc","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant"}}]}`))

		// 2. Reasoning chunk
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"reasoning_content":"Step 1: compute answer."}}]}`))

		// 3. Content chunk 1
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"content":"The answer "}}]}`))

		// 4. Content chunk 2
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"content":"is 42."}}]}`))

		// 5. Tool call start
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_99","type":"function","function":{"name":"calc","arguments":"{\"expr\":"}}]}}]}`))

		// 6. Tool call arg continuation
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"6*7\"}"}}]}}]}`))

		// 7. Finish reason & usage
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-abc","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":50,"completion_tokens":25,"total_tokens":75,"prompt_tokens_details":{"cached_tokens":10},"completion_tokens_details":{"reasoning_tokens":15}}}`))

		diag := parser.Finalize(7, 300, 200)
		summaryBytes := parser.BuildStreamSummary(7, diag)

		var summary proxy.StreamSummary
		if err := json.Unmarshal(summaryBytes, &summary); err != nil {
			t.Fatalf("failed to unmarshal summary JSON: %v", err)
		}

		if !summary.StreamReconstructed {
			t.Errorf("expected StreamReconstructed=true")
		}
		if summary.APIFormat != "chat" {
			t.Errorf("expected APIFormat='chat', got %q", summary.APIFormat)
		}
		if summary.ServedModel != "gpt-4o-mini" {
			t.Errorf("expected ServedModel='gpt-4o-mini', got %q", summary.ServedModel)
		}
		if summary.Chunks != 7 {
			t.Errorf("expected Chunks=7, got %d", summary.Chunks)
		}
		if summary.StopReason != "tool_calls" {
			t.Errorf("expected StopReason='tool_calls', got %q", summary.StopReason)
		}
		if !summary.ThinkingDelivered {
			t.Errorf("expected ThinkingDelivered=true")
		}
		if summary.Thinking != "Step 1: compute answer." {
			t.Errorf("expected Thinking='Step 1: compute answer.', got %q", summary.Thinking)
		}
		if summary.Content != "The answer is 42." {
			t.Errorf("expected Content='The answer is 42.', got %q", summary.Content)
		}
		if len(summary.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(summary.ToolCalls))
		}
		if summary.ToolCalls[0].ID != "call_99" || summary.ToolCalls[0].Name != "calc" || summary.ToolCalls[0].Arguments != `{"expr":"6*7"}` {
			t.Errorf("unexpected tool call: %+v", summary.ToolCalls[0])
		}
		if summary.Usage.PromptTokens != 50 || summary.Usage.CompletionTokens != 25 || summary.Usage.TotalTokens != 75 {
			t.Errorf("unexpected usage: %+v", summary.Usage)
		}
		if summary.Usage.CacheReadTokens != 10 || summary.Usage.ReasoningTokens != 15 {
			t.Errorf("unexpected cache/reasoning usage: %+v", summary.Usage)
		}
	})

	t.Run("Omit empty thinking and tool calls", func(t *testing.T) {
		parser := proxy.NewStreamDiagnosticsParser("chat")
		parser.FeedSSEEvent(nil, []byte(`{"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hi!","role":"assistant"},"finish_reason":"stop"}]}`))

		diag := parser.Finalize(1, 100, 200)
		summaryBytes := parser.BuildStreamSummary(1, diag)

		var raw map[string]interface{}
		_ = json.Unmarshal(summaryBytes, &raw)

		if _, hasThinking := raw["thinking"]; hasThinking {
			t.Errorf("expected 'thinking' to be omitted when empty")
		}
		if _, hasToolCalls := raw["tool_calls"]; hasToolCalls {
			t.Errorf("expected 'tool_calls' to be omitted when empty")
		}
		if raw["content"] != "Hi!" {
			t.Errorf("expected content 'Hi!', got %v", raw["content"])
		}
	})
}
