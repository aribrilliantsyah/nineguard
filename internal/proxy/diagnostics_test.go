package proxy_test

import (
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
