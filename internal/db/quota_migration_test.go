package db_test

import (
	"testing"
	"nineguard/internal/db"
)

func TestQuotaColumnsAndSettingsSeeded(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	// Verify columns exist on api_keys
	rows, err := d.Query("SELECT quota_limit, quota_period FROM api_keys WHERE 1=0")
	if err != nil {
		t.Errorf("api_keys quota columns missing: %v", err)
	} else {
		rows.Close()
	}

	// Verify heavy_token_threshold setting seeded
	var val string
	err = d.QueryRow("SELECT value FROM settings WHERE key = 'heavy_token_threshold'").Scan(&val)
	if err != nil || val != "8000" {
		t.Errorf("expected heavy_token_threshold '8000', got %q, err: %v", val, err)
	}
}
