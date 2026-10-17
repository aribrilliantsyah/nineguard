package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nineguard/internal/upstream"
)

func extractMessageText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					sb.WriteString(text)
				}
			}
		}
		return sb.String()
	default:
		return ""
	}
}

// IsChatCommand checks if the latest user message in the conversation is a slash command.
func IsChatCommand(messages []chatMessage) (command string, arg string, isCmd bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			rawText := extractMessageText(messages[i].Content)
			fields := strings.Fields(strings.TrimSpace(rawText))
			if len(fields) > 0 {
				cmd := strings.ToLower(fields[0])
				switch cmd {
				case "/quota", "/limits", "/usage":
					subArg := ""
					if len(fields) > 1 {
						subArg = strings.ToLower(fields[1])
					}
					return cmd, subArg, true
				}
			}
			break
		}
	}
	return "", "", false
}

func formatTimeAgo(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "just now"
	}
	diff := time.Since(*t)
	if diff < 1*time.Minute {
		return "just now"
	}
	if diff < 1*time.Hour {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(diff.Hours()))
}

func formatResetIn(t *time.Time) string {
	if t == nil {
		return "-"
	}
	diff := time.Until(*t)
	if diff <= 0 {
		return "now"
	}

	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("in %dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("in %dh %dm", hours, minutes)
	}
	return fmt.Sprintf("in %dm", minutes)
}

// FormatQuotaMarkdown builds a human-readable Markdown response from quota data.
func FormatQuotaMarkdown(resp *upstream.UnifiedQuotaResponse, arg string) string {
	var sb strings.Builder
	sb.WriteString("📊 **NineGuard Quota Report**\n\n")

	showKey := arg == "" || arg == "key"
	showUpstream := arg == "" || arg != "key"
	filterStr := ""
	if arg != "" && arg != "key" && arg != "upstream" {
		filterStr = strings.ToLower(arg)
	}

	// 1. Client key budget
	if showKey {
		if resp.KeyQuota != nil {
			kq := resp.KeyQuota
			warnIcon := ""
			if kq.Warning {
				warnIcon = "⚠️ "
			}
			sb.WriteString("### 👤 Key Allowance (Admin Limit)\n\n")
			sb.WriteString(fmt.Sprintf("**Token Budget:** %s%s / %s tokens remaining (%s",
				warnIcon,
				formatTokenCount(kq.Remaining),
				formatTokenCount(kq.Limit),
				kq.Period,
			))
			if kq.ResetAt != nil {
				sb.WriteString(fmt.Sprintf(", resets %s", formatResetIn(kq.ResetAt)))
			}
			sb.WriteString(")\n\n")
		} else if len(resp.ModelQuotas) > 0 {
			sb.WriteString("### 👤 Key Allowance (Admin Model Limits)\n\n")
			sb.WriteString("| Model | Limit | Used | Remaining | Period | Reset |\n")
			sb.WriteString("|---|---|---|---|---|---|\n")
			for _, mq := range resp.ModelQuotas {
				warnIcon := ""
				if mq.Warning {
					warnIcon = "⚠️ "
				}
				sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s%s | %s | %s |\n",
					mq.ModelID,
					formatTokenCount(mq.Limit),
					formatTokenCount(mq.Used),
					warnIcon,
					formatTokenCount(mq.Remaining),
					mq.Period,
					formatResetIn(mq.ResetAt),
				))
			}
			sb.WriteString("\n")
		}
	}

	if !showUpstream {
		return sb.String()
	}

	// 2. Upstream provider pools
	var matchingPools []upstream.MergedPoolQuota
	for _, pool := range resp.UpstreamPools {
		if filterStr != "" {
			matches := strings.Contains(strings.ToLower(pool.Provider), filterStr) ||
				strings.Contains(strings.ToLower(pool.SubProvider), filterStr) ||
				strings.Contains(strings.ToLower(pool.Family), filterStr)
			if !matches {
				for _, b := range pool.Buckets {
					if strings.Contains(strings.ToLower(b.Name), filterStr) {
						matches = true
						break
					}
				}
			}
			if !matches {
				continue
			}
		}
		matchingPools = append(matchingPools, pool)
	}

	if len(matchingPools) == 0 {
		if filterStr != "" {
			sb.WriteString(fmt.Sprintf("*No upstream pools matching '%s'.*\n", filterStr))
		} else {
			sb.WriteString("*No active upstream quota trackers configured.*\n")
		}
		return sb.String()
	}

	sb.WriteString("### 🌐 Upstream Provider Capacity (Shared Pool)\n\n")
	sb.WriteString("| Provider / Family | Window | Used / Total | Remaining | Next Reset |\n")
	sb.WriteString("|---|---|---|---|---|\n")

	for _, pool := range matchingPools {
		displayFamily := pool.Family
		if displayFamily == "" {
			displayFamily = pool.Provider
		}

		for _, b := range pool.Buckets {
			if filterStr != "" && !strings.Contains(strings.ToLower(pool.Provider), filterStr) &&
				!strings.Contains(strings.ToLower(pool.SubProvider), filterStr) &&
				!strings.Contains(strings.ToLower(pool.Family), filterStr) &&
				!strings.Contains(strings.ToLower(b.Name), filterStr) {
				continue
			}

			warnIcon := ""
			if b.Warning {
				warnIcon = "⚠️ "
			}

			usedTotal := fmt.Sprintf("%s / %s", formatTokenCount(b.Used), formatTokenCount(b.Total))
			if b.IsCreditBalance {
				usedTotal = fmt.Sprintf("$%.2f", float64(b.Remaining)/100.0)
			} else if b.Total <= 0 {
				usedTotal = "-"
			}

			remPct := fmt.Sprintf("%s%d%%", warnIcon, b.RemainingPercentage)
			if b.IsCreditBalance {
				remPct = "Active"
			}

			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				displayFamily,
				b.Name,
				usedTotal,
				remPct,
				formatResetIn(b.ResetAt),
			))
		}
	}

	if resp.LastUpdated != nil && !resp.LastUpdated.IsZero() {
		interval := resp.SyncInterval
		if interval == "" {
			interval = upstream.FormatInterval(upstream.DefaultSyncInterval)
		}
		sb.WriteString(fmt.Sprintf("\n*Upstream pools last checked %s · Auto-refreshes every %s*\n", formatTimeAgo(resp.LastUpdated), interval))
	}

	return sb.String()
}

// WriteChatCommandResponse sends a synthetic chat completion matching OpenAI protocol.
func WriteChatCommandResponse(w http.ResponseWriter, command, markdown string, stream bool) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	cmdID := "chatcmpl-ng-cmd-" + hex.EncodeToString(b)
	created := time.Now().Unix()

	if !stream {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := map[string]interface{}{
			"id":      cmdID,
			"object":  "chat.completion",
			"created": created,
			"model":   "nineguard/system",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": markdown,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     0,
				"completion_tokens": 0,
				"total_tokens":      0,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Streaming SSE chunks
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	// Chunk 1: Content
	chunk1 := map[string]interface{}{
		"id":      cmdID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   "nineguard/system",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"role":    "assistant",
					"content": markdown,
				},
				"finish_reason": nil,
			},
		},
	}
	data1, _ := json.Marshal(chunk1)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data1)
	if canFlush {
		flusher.Flush()
	}

	// Chunk 2: Finish
	chunk2 := map[string]interface{}{
		"id":      cmdID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   "nineguard/system",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": "stop",
			},
		},
	}
	data2, _ := json.Marshal(chunk2)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data2)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}
