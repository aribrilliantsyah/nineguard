package plugins

import "testing"

func TestGuidancePresence(t *testing.T) {
	for _, id := range []string{"headroom", "caveman", "ponytail"} {
		g, ok := BuiltinGuidance[id]
		if !ok || g.Summary == "" {
			t.Errorf("missing guidance for built-in %s", id)
		}
	}
}
