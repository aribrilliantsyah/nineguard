package builtin

import (
	"strings"

	"nineguard/internal/plugins/builtin/prompts"
)

const PonytailMarker = "[nineguard:ponytail]"

// ApplyPonytail prepends a system message at index 0 with the Ponytail prompt.
func ApplyPonytail(messages []map[string]any, settings map[string]any) (newMessages []map[string]any, skipped bool, skipReason string, overhead int, err error) {
	if HasMarker(messages, PonytailMarker) {
		return messages, true, "marker_present", 0, nil
	}

	prompt := prompts.PonytailPrompt
	level := "full"
	if settings != nil {
		if lvl, ok := settings["level"].(string); ok && lvl != "" {
			level = strings.ToLower(strings.TrimSpace(lvl))
		}
	}

	switch level {
	case "lite":
		prompt = "Level: lite. Preserve mild compression, keep explanations concise.\n" + prompts.PonytailPrompt
	case "ultra":
		prompt = "Level: ultra. Maximum brevity, no pleasantries, concise syntax.\n" + prompts.PonytailPrompt
	default:
		prompt = prompts.PonytailPrompt
	}

	if settings != nil {
		if override, ok := settings["prompt_override"].(string); ok && strings.TrimSpace(override) != "" {
			prompt = strings.TrimSpace(override)
		}
	}

	injectedText := PonytailMarker + "\n" + prompt
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
