package builtin

import (
	"regexp"
	"strings"
)

var (
	// Regex for private keys
	rePrivateKey = regexp.MustCompile(`(?m)-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)
	// Regex for high-entropy API tokens (OpenAI, GitHub, AWS)
	reAPIKey = regexp.MustCompile(`\b(?:sk-[a-zA-Z0-9_\-]{20,}|ghp_[a-zA-Z0-9]{36}|AKIA[0-9A-Z]{16})\b`)
	// Regex for common environment variable secrets
	reEnvSecret = regexp.MustCompile(`(?i)\b(?:AWS_SECRET_ACCESS_KEY|DATABASE_URL|PRIVATE_KEY|GITHUB_TOKEN)\s*=\s*[^\s]+`)
)

func redactText(text string) (string, int) {
	count := 0
	text = rePrivateKey.ReplaceAllStringFunc(text, func(m string) string {
		count++
		return "[REDACTED_SECRET:PRIVATE_KEY]"
	})
	text = reAPIKey.ReplaceAllStringFunc(text, func(m string) string {
		count++
		return "[REDACTED_SECRET:API_KEY]"
	})
	text = reEnvSecret.ReplaceAllStringFunc(text, func(m string) string {
		count++
		return "[REDACTED_SECRET:ENV_SECRET]"
	})
	return text, count
}

func countSecrets(text string) int {
	matches := len(rePrivateKey.FindAllString(text, -1))
	matches += len(reAPIKey.FindAllString(text, -1))
	matches += len(reEnvSecret.FindAllString(text, -1))
	return matches
}

// ApplySecretGuard inspects messages for secrets and either blocks or redacts them.
func ApplySecretGuard(messages []map[string]any, settings map[string]any) (newMessages []map[string]any, skipped bool, rejected bool, rejectMsg string, err error) {
	action := "block"
	if settings != nil {
		if a, ok := settings["action"].(string); ok && a != "" {
			action = strings.ToLower(strings.TrimSpace(a))
		}
	}

	totalSecrets := 0
	// 1. Scan for secrets
	for _, m := range messages {
		if contentStr, ok := m["content"].(string); ok {
			totalSecrets += countSecrets(contentStr)
		} else if parts, ok := m["content"].([]any); ok {
			for _, part := range parts {
				if partMap, ok := part.(map[string]any); ok {
					if textStr, ok := partMap["text"].(string); ok {
						totalSecrets += countSecrets(textStr)
					}
				}
			}
		}
	}

	if totalSecrets == 0 {
		return messages, true, false, "", nil
	}

	if action == "block" {
		return messages, false, true, "Request blocked: prompt contains sensitive credentials or secrets (SecretGuard).", nil
	}

	if action == "redact" {
		redactedMsgs := make([]map[string]any, len(messages))
		for i, m := range messages {
			cloned := make(map[string]any, len(m))
			for k, v := range m {
				cloned[k] = v
			}
			if contentStr, ok := m["content"].(string); ok {
				redacted, _ := redactText(contentStr)
				cloned["content"] = redacted
			} else if parts, ok := m["content"].([]any); ok {
				newParts := make([]any, len(parts))
				for j, part := range parts {
					if partMap, ok := part.(map[string]any); ok {
						clonedPart := make(map[string]any, len(partMap))
						for pk, pv := range partMap {
							clonedPart[pk] = pv
						}
						if textStr, ok := partMap["text"].(string); ok {
							redacted, _ := redactText(textStr)
							clonedPart["text"] = redacted
						}
						newParts[j] = clonedPart
					} else {
						newParts[j] = part
					}
				}
				cloned["content"] = newParts
			}
			redactedMsgs[i] = cloned
		}
		return redactedMsgs, false, false, "", nil
	}

	// warn_only or unknown: pass unmodified
	return messages, false, false, "", nil
}
