package proxy

import (
	"encoding/json"
	"strings"
)

// UsageDiagnostics captures normalized token, caching, thinking, and model facts.
type UsageDiagnostics struct {
	ApiFormat         string
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	CacheReadTokens   int
	CacheWriteTokens  int
	ReasoningTokens   int
	ThinkingRequested bool
	ThinkingDelivered bool
	ServedModel       string
	UsageEstimated    bool
	ErrorMessage      *string
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
			for _, o := range responsesResp.Output {
				if o.Type == "reasoning" {
					diag.ThinkingDelivered = true
					break
				}
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
			if chatResp.Error != nil && chatResp.Error.Message != "" {
				diag.ErrorMessage = &chatResp.Error.Message
			}
		}
	}

	return diag
}

// StreamDiagnosticsParser accumulates usage and diagnostics facts from streaming events.
type StreamDiagnosticsParser struct {
	format           string
	servedModel      string
	promptTokens     int
	completionTokens int
	totalTokens      int
	cacheReadTokens  int
	cacheWriteTokens int
	reasoningTokens  int
	thinkingDelivered bool
	hasRealUsage     bool
	anthropicInput   int
}

// NewStreamDiagnosticsParser creates an accumulator for the given API format.
func NewStreamDiagnosticsParser(format string) *StreamDiagnosticsParser {
	return &StreamDiagnosticsParser{
		format: format,
	}
}

// FeedSSEEvent parses an individual SSE event (raw bytes and extracted data payload).
func (p *StreamDiagnosticsParser) FeedSSEEvent(eventRaw []byte, dataPayload []byte) {
	if len(dataPayload) == 0 || string(dataPayload) == "[DONE]" {
		return
	}

	switch p.format {
	case "messages":
		var anthropicEvent struct {
			Type    string `json:"type"`
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
			} `json:"content_block"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
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
				}
			} else if anthropicEvent.Type == "message_delta" {
				if anthropicEvent.Usage.OutputTokens > 0 {
					p.completionTokens = anthropicEvent.Usage.OutputTokens
					p.hasRealUsage = true
				}
			}
		}

	case "responses":
		var respEvent struct {
			Response struct {
				Model string `json:"model"`
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
		if err := json.Unmarshal(dataPayload, &respEvent); err == nil && respEvent.Response.Usage.TotalTokens > 0 {
			if respEvent.Response.Model != "" {
				p.servedModel = respEvent.Response.Model
			}
			p.promptTokens = respEvent.Response.Usage.InputTokens
			p.completionTokens = respEvent.Response.Usage.OutputTokens
			p.totalTokens = respEvent.Response.Usage.TotalTokens
			p.cacheReadTokens = respEvent.Response.Usage.InputTokensDetails.CachedTokens
			p.reasoningTokens = respEvent.Response.Usage.OutputTokensDetails.ReasoningTokens
			if p.reasoningTokens > 0 {
				p.thinkingDelivered = true
			}
			p.hasRealUsage = true
		}

	default: // "chat"
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					ReasoningContent string          `json:"reasoning_content"`
					Reasoning        json.RawMessage `json:"reasoning"`
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
				if choice.Delta.ReasoningContent != "" || len(choice.Delta.Reasoning) > 0 {
					p.thinkingDelivered = true
				}
			}
			if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
				p.promptTokens = chunk.Usage.PromptTokens
				p.completionTokens = chunk.Usage.CompletionTokens
				p.totalTokens = chunk.Usage.TotalTokens
				p.cacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
				p.cacheWriteTokens = chunk.Usage.PromptTokensDetails.CacheWriteTokens
				p.reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
				if p.reasoningTokens > 0 {
					p.thinkingDelivered = true
				}
				p.hasRealUsage = true
			}
		}
	}
}

// Finalize produces the final UsageDiagnostics for a stream.
func (p *StreamDiagnosticsParser) Finalize(chunkCount int, bodyLen int, statusCode int) UsageDiagnostics {
	diag := UsageDiagnostics{
		ApiFormat:         p.format,
		ServedModel:       p.servedModel,
		CacheReadTokens:   p.cacheReadTokens,
		CacheWriteTokens:  p.cacheWriteTokens,
		ReasoningTokens:   p.reasoningTokens,
		ThinkingDelivered: p.thinkingDelivered,
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
