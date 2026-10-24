package builtin

import "strings"

// HasMarker checks if any system or developer message contains the specified marker line.
func HasMarker(messages []map[string]any, marker string) bool {
	for _, m := range messages {
		role, _ := m["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		if contentStr, ok := m["content"].(string); ok {
			if strings.Contains(contentStr, marker) {
				return true
			}
		} else if contentArr, ok := m["content"].([]any); ok {
			for _, part := range contentArr {
				if partMap, ok := part.(map[string]any); ok {
					if text, ok := partMap["text"].(string); ok && strings.Contains(text, marker) {
						return true
					}
				} else if text, ok := part.(string); ok && strings.Contains(text, marker) {
					return true
				}
			}
		}
	}
	return false
}

// EstimateOverhead computes ceil(len(text) / 4)
func EstimateOverhead(text string) int {
	l := len(text)
	if l == 0 {
		return 0
	}
	return (l + 3) / 4
}

// HasSystemMarker checks if the top-level Anthropic system parameter contains marker.
func HasSystemMarker(system any, marker string) bool {
	if system == nil {
		return false
	}
	switch v := system.(type) {
	case string:
		return strings.Contains(v, marker)
	case []any:
		for _, part := range v {
			if partMap, ok := part.(map[string]any); ok {
				if text, ok := partMap["text"].(string); ok && strings.Contains(text, marker) {
					return true
				}
			} else if text, ok := part.(string); ok && strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
}

// InjectSystemPrompt polymorphically injects a prompt into the top-level Anthropic system field.
func InjectSystemPrompt(body map[string]any, marker, prompt string) (modifiedBody map[string]any, skipped bool, skipReason string, overhead int, err error) {
	sysVal := body["system"]
	if HasSystemMarker(sysVal, marker) {
		return body, true, "marker_present", 0, nil
	}

	injectedText := marker + "\n" + prompt
	overhead = EstimateOverhead(injectedText)

	cloned := make(map[string]any, len(body)+1)
	for k, v := range body {
		cloned[k] = v
	}

	switch v := sysVal.(type) {
	case nil:
		cloned["system"] = injectedText
	case string:
		if strings.TrimSpace(v) == "" {
			cloned["system"] = injectedText
		} else {
			cloned["system"] = injectedText + "\n\n" + v
		}
	case []any:
		newBlock := map[string]any{
			"type": "text",
			"text": injectedText,
		}
		newArr := make([]any, 0, len(v)+1)
		newArr = append(newArr, newBlock)
		newArr = append(newArr, v...)
		cloned["system"] = newArr
	default:
		cloned["system"] = injectedText
	}
	return cloned, false, "", overhead, nil
}
