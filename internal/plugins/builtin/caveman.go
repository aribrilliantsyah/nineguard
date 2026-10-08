package builtin

import (
	"strings"

	"nineguard/internal/plugins/builtin/prompts"
)

const CavemanMarker = "[nineguard:caveman]"

// ApplyCaveman prepends a system message at index 0 with the Caveman prompt.
func ApplyCaveman(messages []map[string]any, settings map[string]any) (newMessages []map[string]any, skipped bool, skipReason string, overhead int, err error) {
	if HasMarker(messages, CavemanMarker) {
		return messages, true, "marker_present", 0, nil
	}

	prompt := prompts.CavemanPrompt
	variant := "caveman"
	if settings != nil {
		if v, ok := settings["variant"].(string); ok && v != "" {
			variant = strings.ToLower(strings.TrimSpace(v))
		}
	}

	switch variant {
	case "ultracave":
		prompt = prompts.UltraCavePrompt
	case "megacave":
		prompt = prompts.MegaCavePrompt
	default:
		prompt = prompts.CavemanPrompt
	}

	if settings != nil {
		if override, ok := settings["prompt_override"].(string); ok && strings.TrimSpace(override) != "" {
			prompt = strings.TrimSpace(override)
		}
	}

	injectedText := CavemanMarker + "\n" + prompt
	overhead = EstimateOverhead(injectedText)

	sysMsg := map[string]any{
		"role":    "system",
		"content": injectedText,
	}

	newMessages = make([]map[string]any, 0, len(messages)+1)
	newMessages = append(newMessages, sysMsg)
	newMessages = append(newMessages, messages...)
	return newMessages, false, "", overhead, nil
}
