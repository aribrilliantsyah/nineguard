package prompts

import (
	"strings"
	"testing"
)

func TestEmbeddedPrompts(t *testing.T) {
	if !strings.Contains(CavemanPrompt, "terse caveman") {
		t.Errorf("expected CavemanPrompt to contain 'terse caveman', got %q", CavemanPrompt)
	}
	if !strings.Contains(UltraCavePrompt, "Ultra terse") {
		t.Errorf("expected UltraCavePrompt to contain 'Ultra terse', got %q", UltraCavePrompt)
	}
	if !strings.Contains(MegaCavePrompt, "文言文") {
		t.Errorf("expected MegaCavePrompt to contain classical Chinese marker, got %q", MegaCavePrompt)
	}
	if !strings.Contains(PonytailPrompt, "concise") {
		t.Errorf("expected PonytailPrompt to contain 'concise', got %q", PonytailPrompt)
	}
}
