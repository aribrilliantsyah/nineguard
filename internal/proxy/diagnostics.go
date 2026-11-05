package proxy

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// UsageDiagnostics captures normalized token, caching, thinking, and model facts.
type UsageDiagnostics struct {
	ApiFormat          string
	PromptTokens       int
	CompletionTokens   int
	TotalTokens        int
	CacheReadTokens    int
	CacheWriteTokens   int
	ReasoningTokens    int
	ThinkingRequested  bool
	ThinkingDelivered  bool
	ThinkingVerifiable bool
	ServedModel        string
	UsageEstimated     bool
	ErrorMessage       *string
}

// StreamToolCall captures an accumulated tool call from a stream.
type StreamToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// StreamUsageSummary captures token usage counts for the reconstructed stream summary.
type StreamUsageSummary struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
}

// StreamSummary represents the reconstructed structured summary of an SSE stream response.
type StreamSummary struct {
	StreamReconstructed bool               `json:"_stream_reconstructed"`
	APIFormat           string             `json:"api_format"`
	ServedModel         string             `json:"served_model"`
	Chunks              int                `json:"chunks"`
	StopReason          string             `json:"stop_reason,omitempty"`
	ThinkingDelivered   bool               `json:"thinking_delivered"`
	Thinking            string             `json:"thinking,omitempty"`
	Content             string             `json:"content"`
	ToolCalls           []StreamToolCall   `json:"tool_calls,omitempty"`
	Usage               StreamUsageSummary `json:"usage"`
}

// DetectAPIFormat classifies a request path into "chat", "messages", "responses", or "other".
func DetectAPIFormat(path string) string {
	clean := strings.TrimRight(strings.ToLower(path), "/")
	if strings.HasSuffix(clean, "/chat/completions") {
		return "chat"
	}
	if strings.HasSuffix(clean, "/messages") {
		return "messages"
	}
	if strings.HasSuffix(clean, "/responses") {
		return "responses"
	}
	return "other"
}

// DetectThinkingRequested checks whether the client requested reasoning/thinking.
func DetectThinkingRequested(format string, body []byte, modelName string) bool {
	if strings.Contains(strings.ToLower(modelName), "thinking") {
		return true
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}

	// 1. Check reasoning_effort (string)
	if effortRaw, ok := raw["reasoning_effort"]; ok {
		var effort string
		if err := json.Unmarshal(effortRaw, &effort); err == nil {
			if effort != "" && !strings.EqualFold(effort, "none") {
				return true
			}
		}
	}

	// 2. Check reasoning (object)
	if rRaw, ok := raw["reasoning"]; ok {
		var rObj struct {
			Enabled   *bool   `json:"enabled"`
			Effort    string  `json:"effort"`
			MaxTokens int     `json:"max_tokens"`
		}
		if err := json.Unmarshal(rRaw, &rObj); err == nil {
			if rObj.Enabled != nil && *rObj.Enabled {
				return true
			}
			if rObj.Effort != "" && !strings.EqualFold(rObj.Effort, "none") {
				return true
			}
			if rObj.MaxTokens > 0 {
				return true
			}
		}
	}

	// 3. Check thinking (object, Anthropic or OpenAI thinking config)
	if tRaw, ok := raw["thinking"]; ok {
		var tObj struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(tRaw, &tObj); err == nil {
			typeLower := strings.ToLower(tObj.Type)
			if typeLower == "enabled" || typeLower == "adaptive" {
				return true
			}
		}
	}

	return false
}

// ParseNonStreamDiagnostics extracts usage diagnostics from a non-streaming response body.
func ParseNonStreamDiagnostics(format string, body []byte) UsageDiagnostics {
	var diag UsageDiagnostics
	diag.ApiFormat = format

	switch format {
	case "messages":
		diag.ThinkingVerifiable = true
		var anthropicResp struct {
			Model   string `json:"model"`
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &anthropicResp); err == nil {
			diag.ServedModel = anthropicResp.Model
			diag.CacheReadTokens = anthropicResp.Usage.CacheReadInputTokens
			diag.CacheWriteTokens = anthropicResp.Usage.CacheCreationInputTokens
			diag.CompletionTokens = anthropicResp.Usage.OutputTokens
			diag.PromptTokens = anthropicResp.Usage.InputTokens + diag.CacheReadTokens + diag.CacheWriteTokens
			diag.TotalTokens = diag.PromptTokens + diag.CompletionTokens

			for _, c := range anthropicResp.Content {
				if c.Type == "thinking" || c.Type == "redacted_thinking" {
					diag.ThinkingDelivered = true
					break
				}
			}
			if anthropicResp.Error != nil && anthropicResp.Error.Message != "" {
				diag.ErrorMessage = &anthropicResp.Error.Message
			}
		}

	case "responses":
		var responsesResp struct {
			Model  string `json:"model"`
			Output []struct {
				Type string `json:"type"`
			} `json:"output"`
			Usage struct {
				InputTokens        int `json:"input_tokens"`
				OutputTokens       int `json:"output_tokens"`
				TotalTokens        int `json:"total_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutputTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &responsesResp); err == nil {
			diag.ServedModel = responsesResp.Model
			diag.PromptTokens = responsesResp.Usage.InputTokens
			diag.CompletionTokens = responsesResp.Usage.OutputTokens
			diag.TotalTokens = responsesResp.Usage.TotalTokens
			diag.CacheReadTokens = responsesResp.Usage.InputTokensDetails.CachedTokens
			diag.ReasoningTokens = responsesResp.Usage.OutputTokensDetails.ReasoningTokens
			if diag.ReasoningTokens > 0 {
				diag.ThinkingDelivered = true
			}
			if responsesResp.Usage.OutputTokensDetails.ReasoningTokens > 0 || len(responsesResp.Output) > 0 {
				for _, o := range responsesResp.Output {
					if o.Type == "reasoning" {
						diag.ThinkingDelivered = true
						diag.ThinkingVerifiable = true
						break
					}
				}
			}
			// Check if output_tokens_details is present in json
			var rawMap map[string]json.RawMessage
			if err := json.Unmarshal(body, &rawMap); err == nil {
				if usageRaw, ok := rawMap["usage"]; ok {
					var usageMap map[string]json.RawMessage
					if err := json.Unmarshal(usageRaw, &usageMap); err == nil {
						if _, ok := usageMap["output_tokens_details"]; ok {
							diag.ThinkingVerifiable = true
						}
					}
				}
			}
			if diag.ThinkingDelivered {
				diag.ThinkingVerifiable = true
			}
			if responsesResp.Error != nil && responsesResp.Error.Message != "" {
				diag.ErrorMessage = &responsesResp.Error.Message
			}
		}

	default: // "chat" and "other"
		var chatResp struct {
			Model   string `json:"model"`
			Choices []struct {
				Message struct {
					ReasoningContent string          `json:"reasoning_content"`
					Reasoning        json.RawMessage `json:"reasoning"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens     int `json:"cached_tokens"`
					CacheWriteTokens int `json:"cache_write_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &chatResp); err == nil {
			diag.ServedModel = chatResp.Model
			diag.PromptTokens = chatResp.Usage.PromptTokens
			diag.CompletionTokens = chatResp.Usage.CompletionTokens
			diag.TotalTokens = chatResp.Usage.TotalTokens
			diag.CacheReadTokens = chatResp.Usage.PromptTokensDetails.CachedTokens
			diag.CacheWriteTokens = chatResp.Usage.PromptTokensDetails.CacheWriteTokens
			diag.ReasoningTokens = chatResp.Usage.CompletionTokensDetails.ReasoningTokens
			if diag.ReasoningTokens > 0 {
				diag.ThinkingDelivered = true
			}
			for _, choice := range chatResp.Choices {
				if choice.Message.ReasoningContent != "" || len(choice.Message.Reasoning) > 0 {
					diag.ThinkingDelivered = true
					break
				}
			}
			// Check if completion_tokens_details is present in json
			var rawMap map[string]json.RawMessage
			if err := json.Unmarshal(body, &rawMap); err == nil {
				if usageRaw, ok := rawMap["usage"]; ok {
					var usageMap map[string]json.RawMessage
					if err := json.Unmarshal(usageRaw, &usageMap); err == nil {
						if _, ok := usageMap["completion_tokens_details"]; ok {
							diag.ThinkingVerifiable = true
						}
					}
				}
			}
			if diag.ThinkingDelivered {
				diag.ThinkingVerifiable = true
			}
			if chatResp.Error != nil && chatResp.Error.Message != "" {
				diag.ErrorMessage = &chatResp.Error.Message
			}
		}
	}

	return diag
}

// StreamDiagnosticsParser accumulates usage and diagnostics facts from streaming events.
type StreamDiagnosticsParser struct {
	format            string
	servedModel       string
	promptTokens      int
	completionTokens  int
	totalTokens       int
	cacheReadTokens   int
	cacheWriteTokens   int
	reasoningTokens    int
	thinkingDelivered  bool
	thinkingVerifiable bool
	hasRealUsage       bool
	anthropicInput    int

	contentBuilder  strings.Builder
	thinkingBuilder strings.Builder
	stopReason      string

	msgToolBlocks map[int]*StreamToolCall
	msgToolOrder  []int

	chatToolCalls map[int]*StreamToolCall
	chatToolOrder []int
}

// NewStreamDiagnosticsParser creates an accumulator for the given API format.
func NewStreamDiagnosticsParser(format string) *StreamDiagnosticsParser {
	p := &StreamDiagnosticsParser{
		format:        format,
		msgToolBlocks: make(map[int]*StreamToolCall),
		chatToolCalls: make(map[int]*StreamToolCall),
	}
	if format == "messages" {
		p.thinkingVerifiable = true
	}
	return p
}

func appendWithCap(sb *strings.Builder, s string, maxBytes int) {
	if sb.Len() >= maxBytes || s == "" {
		return
	}
	remaining := maxBytes - sb.Len()
	if len(s) > remaining {
		sub := s[:remaining]
		for len(sub) > 0 && !utf8.ValidString(sub) {
			sub = sub[:len(sub)-1]
		}
		sb.WriteString(sub)
	} else {
		sb.WriteString(s)
	}
}

func appendToolArgWithCap(tc *StreamToolCall, s string, maxBytes int) {
	if len(tc.Arguments) >= maxBytes || s == "" {
		return
	}
	remaining := maxBytes - len(tc.Arguments)
	if len(s) > remaining {
		sub := s[:remaining]
		for len(sub) > 0 && !utf8.ValidString(sub) {
			sub = sub[:len(sub)-1]
		}
		tc.Arguments += sub
	} else {
		tc.Arguments += s
	}
}

// FeedSSEEvent parses an individual SSE event (raw bytes and extracted data payload).
func (p *StreamDiagnosticsParser) FeedSSEEvent(eventRaw []byte, dataPayload []byte) {
	if len(dataPayload) == 0 || string(dataPayload) == "[DONE]" {
		return
	}

	if p.msgToolBlocks == nil {
		p.msgToolBlocks = make(map[int]*StreamToolCall)
	}
	if p.chatToolCalls == nil {
		p.chatToolCalls = make(map[int]*StreamToolCall)
	}

	switch p.format {
	case "messages":
		var anthropicEvent struct {
			Type    string `json:"type"`
			Index   *int   `json:"index,omitempty"`
			Message struct {
				Model string `json:"model"`
				Usage struct {
					InputTokens              int `json:"input_tokens"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens             int `json:"output_tokens"`
				InputTokens              int `json:"input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal(dataPayload, &anthropicEvent); err == nil {
			if anthropicEvent.Type == "message_start" {
				if anthropicEvent.Message.Model != "" {
					p.servedModel = anthropicEvent.Message.Model
				}
				p.anthropicInput = anthropicEvent.Message.Usage.InputTokens
				p.cacheReadTokens = anthropicEvent.Message.Usage.CacheReadInputTokens
				p.cacheWriteTokens = anthropicEvent.Message.Usage.CacheCreationInputTokens
				p.hasRealUsage = true
			} else if anthropicEvent.Type == "content_block_start" {
				if anthropicEvent.ContentBlock.Type == "thinking" || anthropicEvent.ContentBlock.Type == "redacted_thinking" {
					p.thinkingDelivered = true
				} else if anthropicEvent.ContentBlock.Type == "tool_use" {
					idx := 0
					if anthropicEvent.Index != nil {
						idx = *anthropicEvent.Index
					}
					if _, ok := p.msgToolBlocks[idx]; !ok {
						p.msgToolOrder = append(p.msgToolOrder, idx)
					}
					p.msgToolBlocks[idx] = &StreamToolCall{
						ID:   anthropicEvent.ContentBlock.ID,
						Name: anthropicEvent.ContentBlock.Name,
					}
				}
			} else if anthropicEvent.Type == "content_block_delta" {
				idx := 0
				if anthropicEvent.Index != nil {
					idx = *anthropicEvent.Index
				}
				switch anthropicEvent.Delta.Type {
				case "text_delta":
					appendWithCap(&p.contentBuilder, anthropicEvent.Delta.Text, 64*1024)
				case "thinking_delta":
					p.thinkingDelivered = true
					appendWithCap(&p.thinkingBuilder, anthropicEvent.Delta.Thinking, 32*1024)
				case "input_json_delta":
					if tc, ok := p.msgToolBlocks[idx]; ok {
						appendToolArgWithCap(tc, anthropicEvent.Delta.PartialJSON, 16*1024)
					}
				}
			} else if anthropicEvent.Type == "message_delta" {
				if anthropicEvent.Delta.StopReason != "" {
					p.stopReason = anthropicEvent.Delta.StopReason
				}
				if anthropicEvent.Usage.OutputTokens > 0 {
					p.completionTokens = anthropicEvent.Usage.OutputTokens
					p.hasRealUsage = true
				}
				if p.anthropicInput == 0 && p.cacheReadTokens == 0 && p.cacheWriteTokens == 0 {
					if anthropicEvent.Usage.InputTokens > 0 || anthropicEvent.Usage.CacheReadInputTokens > 0 || anthropicEvent.Usage.CacheCreationInputTokens > 0 {
						p.anthropicInput = anthropicEvent.Usage.InputTokens
						p.cacheReadTokens = anthropicEvent.Usage.CacheReadInputTokens
						p.cacheWriteTokens = anthropicEvent.Usage.CacheCreationInputTokens
						p.hasRealUsage = true
					}
				}
			}
		}

	case "responses":
		var respEvent struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  struct {
				Type string `json:"type"`
			} `json:"item"`
			Response struct {
				Model      string `json:"model"`
				OutputText struct {
					Delta string `json:"delta"`
				} `json:"output_text"`
				Usage struct {
					InputTokens        int `json:"input_tokens"`
					OutputTokens       int `json:"output_tokens"`
					TotalTokens        int `json:"total_tokens"`
					InputTokensDetails struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
					OutputTokensDetails struct {
						ReasoningTokens int `json:"reasoning_tokens"`
					} `json:"output_tokens_details"`
				} `json:"usage"`
			} `json:"response"`
		}
		if err := json.Unmarshal(dataPayload, &respEvent); err == nil {
			if respEvent.Response.Model != "" && p.servedModel == "" {
				p.servedModel = respEvent.Response.Model
			}

			// (a) Reasoning item added or done
			if respEvent.Item.Type == "reasoning" {
				p.thinkingDelivered = true
				p.thinkingVerifiable = true
			}

			// (b) Reasoning text or summary deltas vs output text deltas
			switch respEvent.Type {
			case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
				p.thinkingDelivered = true
				p.thinkingVerifiable = true
				if respEvent.Delta != "" {
					appendWithCap(&p.thinkingBuilder, respEvent.Delta, 32*1024)
				}
			case "response.output_text.delta":
				if respEvent.Delta != "" {
					appendWithCap(&p.contentBuilder, respEvent.Delta, 64*1024)
				}
			default:
				if respEvent.Response.OutputText.Delta != "" {
					appendWithCap(&p.contentBuilder, respEvent.Response.OutputText.Delta, 64*1024)
				} else if respEvent.Delta != "" && (strings.HasSuffix(respEvent.Type, "output_text.delta") || strings.HasSuffix(respEvent.Type, "refusal.delta")) {
					appendWithCap(&p.contentBuilder, respEvent.Delta, 64*1024)
				}
			}

			if respEvent.Response.Usage.TotalTokens > 0 {
				p.promptTokens = respEvent.Response.Usage.InputTokens
				p.completionTokens = respEvent.Response.Usage.OutputTokens
				p.totalTokens = respEvent.Response.Usage.TotalTokens
				p.cacheReadTokens = respEvent.Response.Usage.InputTokensDetails.CachedTokens
				p.reasoningTokens = respEvent.Response.Usage.OutputTokensDetails.ReasoningTokens
				if p.reasoningTokens > 0 {
					p.thinkingDelivered = true
				}
				p.hasRealUsage = true
				// Check if output_tokens_details is present in JSON
				var rawMap map[string]json.RawMessage
				if err := json.Unmarshal(dataPayload, &rawMap); err == nil {
					if respRaw, ok := rawMap["response"]; ok {
						var respMap map[string]json.RawMessage
						if err := json.Unmarshal(respRaw, &respMap); err == nil {
							if usageRaw, ok := respMap["usage"]; ok {
								var usageMap map[string]json.RawMessage
								if err := json.Unmarshal(usageRaw, &usageMap); err == nil {
									if _, ok := usageMap["output_tokens_details"]; ok {
										p.thinkingVerifiable = true
									}
								}
							}
						}
					}
				}
			}
		}

	default: // "chat"
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Index        *int   `json:"index"`
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content"`
					Reasoning        json.RawMessage `json:"reasoning"`
					ToolCalls        []struct {
						Index    *int   `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens     int `json:"cached_tokens"`
					CacheWriteTokens int `json:"cache_write_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}

		if err := json.Unmarshal(dataPayload, &chunk); err == nil {
			if chunk.Model != "" && p.servedModel == "" {
				p.servedModel = chunk.Model
			}
			for _, choice := range chunk.Choices {
				if choice.FinishReason != "" {
					p.stopReason = choice.FinishReason
				}
				if choice.Delta.Content != "" {
					appendWithCap(&p.contentBuilder, choice.Delta.Content, 64*1024)
				}
				if choice.Delta.ReasoningContent != "" {
					p.thinkingDelivered = true
					p.thinkingVerifiable = true
					appendWithCap(&p.thinkingBuilder, choice.Delta.ReasoningContent, 32*1024)
				} else if len(choice.Delta.Reasoning) > 0 {
					p.thinkingDelivered = true
					p.thinkingVerifiable = true
					var rStr string
					if err := json.Unmarshal(choice.Delta.Reasoning, &rStr); err == nil && rStr != "" {
						appendWithCap(&p.thinkingBuilder, rStr, 32*1024)
					}
				}
				for _, tc := range choice.Delta.ToolCalls {
					tcIdx := 0
					if tc.Index != nil {
						tcIdx = *tc.Index
					}
					existing, exists := p.chatToolCalls[tcIdx]
					if !exists {
						existing = &StreamToolCall{
							ID:   tc.ID,
							Name: tc.Function.Name,
						}
						p.chatToolCalls[tcIdx] = existing
						p.chatToolOrder = append(p.chatToolOrder, tcIdx)
					} else {
						if tc.ID != "" {
							existing.ID = tc.ID
						}
						if tc.Function.Name != "" {
							existing.Name = tc.Function.Name
						}
					}
					if tc.Function.Arguments != "" {
						appendToolArgWithCap(existing, tc.Function.Arguments, 16*1024)
					}
				}
			}
			if chunk.Usage != nil && (chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0) {
				p.promptTokens = chunk.Usage.PromptTokens
				p.completionTokens = chunk.Usage.CompletionTokens
				p.totalTokens = chunk.Usage.TotalTokens
				if p.totalTokens == 0 {
					p.totalTokens = p.promptTokens + p.completionTokens
				}
				p.cacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
				p.cacheWriteTokens = chunk.Usage.PromptTokensDetails.CacheWriteTokens
				p.reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
				if p.reasoningTokens > 0 {
					p.thinkingDelivered = true
				}
				p.hasRealUsage = true

				// Check if completion_tokens_details is present in chunk.Usage
				var rawMap map[string]json.RawMessage
				if err := json.Unmarshal(dataPayload, &rawMap); err == nil {
					if usageRaw, ok := rawMap["usage"]; ok {
						var usageMap map[string]json.RawMessage
						if err := json.Unmarshal(usageRaw, &usageMap); err == nil {
							if _, ok := usageMap["completion_tokens_details"]; ok {
								p.thinkingVerifiable = true
							}
						}
					}
				}
			}
		}
	}
}

// BuildStreamSummary constructs a structured JSON summary of the stream response.
func (p *StreamDiagnosticsParser) BuildStreamSummary(chunkCount int, diag UsageDiagnostics) []byte {
	var toolCalls []StreamToolCall
	if p.format == "messages" {
		for _, idx := range p.msgToolOrder {
			if tc, ok := p.msgToolBlocks[idx]; ok {
				toolCalls = append(toolCalls, *tc)
			}
		}
	} else {
		for _, idx := range p.chatToolOrder {
			if tc, ok := p.chatToolCalls[idx]; ok {
				toolCalls = append(toolCalls, *tc)
			}
		}
	}

	model := diag.ServedModel
	if model == "" {
		model = p.servedModel
	}
	format := diag.ApiFormat
	if format == "" {
		format = p.format
	}

	summary := StreamSummary{
		StreamReconstructed: true,
		APIFormat:           format,
		ServedModel:         model,
		Chunks:              chunkCount,
		StopReason:          p.stopReason,
		ThinkingDelivered:   diag.ThinkingDelivered || p.thinkingDelivered,
		Thinking:            p.thinkingBuilder.String(),
		Content:             p.contentBuilder.String(),
		ToolCalls:           toolCalls,
		Usage: StreamUsageSummary{
			PromptTokens:     diag.PromptTokens,
			CompletionTokens: diag.CompletionTokens,
			TotalTokens:      diag.TotalTokens,
			CacheReadTokens:  diag.CacheReadTokens,
			CacheWriteTokens: diag.CacheWriteTokens,
			ReasoningTokens:  diag.ReasoningTokens,
		},
	}

	out, _ := json.Marshal(summary)
	return out
}

// Finalize produces the final UsageDiagnostics for a stream.
func (p *StreamDiagnosticsParser) Finalize(chunkCount int, bodyLen int, statusCode int) UsageDiagnostics {
	diag := UsageDiagnostics{
		ApiFormat:          p.format,
		ServedModel:        p.servedModel,
		CacheReadTokens:    p.cacheReadTokens,
		CacheWriteTokens:   p.cacheWriteTokens,
		ReasoningTokens:    p.reasoningTokens,
		ThinkingDelivered:  p.thinkingDelivered,
		ThinkingVerifiable: p.thinkingVerifiable || p.thinkingDelivered,
	}

	if p.format == "messages" {
		diag.PromptTokens = p.anthropicInput + p.cacheReadTokens + p.cacheWriteTokens
		diag.CompletionTokens = p.completionTokens
		diag.TotalTokens = diag.PromptTokens + diag.CompletionTokens
	} else {
		diag.PromptTokens = p.promptTokens
		diag.CompletionTokens = p.completionTokens
		diag.TotalTokens = p.totalTokens
	}

	if statusCode < 400 {
		if diag.TotalTokens == 0 && chunkCount > 0 {
			diag.CompletionTokens = chunkCount
			if diag.CompletionTokens < 1 {
				diag.CompletionTokens = 1
			}
			diag.PromptTokens = bodyLen / 4
			diag.TotalTokens = diag.PromptTokens + diag.CompletionTokens
			diag.UsageEstimated = true
		} else if diag.TotalTokens == 0 && !p.hasRealUsage {
			diag.UsageEstimated = true
		}
	}

	return diag
}
