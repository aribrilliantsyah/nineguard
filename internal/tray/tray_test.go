package tray

import (
	"testing"
)

func TestTrayIsSupported(t *testing.T) {
	supported := IsSupported()
	t.Logf("IsSupported on this environment: %v", supported)
}
