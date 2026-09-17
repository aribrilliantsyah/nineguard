package tray

import (
	"os"
	"testing"
)

func TestTrayIsSupported(t *testing.T) {
	supported := IsSupported()
	t.Logf("IsSupported on this environment: %v", supported)
}

func TestTrayModeName(t *testing.T) {
	mode := ModeName()
	if mode == "" {
		t.Error("expected non-empty mode name")
	}
	t.Logf("Current ModeName: %s", mode)
}

func TestTrayHasDesktopEnvironment(t *testing.T) {
	hasDE := HasDesktopEnvironment()
	t.Logf("HasDesktopEnvironment: %v", hasDE)
}

func TestTrayOverrideNoTray(t *testing.T) {
	orig := os.Getenv("NINEGUARD_NO_TRAY")
	defer os.Setenv("NINEGUARD_NO_TRAY", orig)

	os.Setenv("NINEGUARD_NO_TRAY", "1")
	if IsSupported() {
		t.Error("expected IsSupported to be false when NINEGUARD_NO_TRAY=1")
	}
}

