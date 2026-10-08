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
