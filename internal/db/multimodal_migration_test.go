package db_test

import (
	"testing"
	"nineguard/internal/db"
)

func TestMultimodalColumns(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	// Verify columns exist on traffic_logs
	rows, err := d.Query("SELECT has_images, image_count FROM traffic_logs WHERE 1=0")
	if err != nil {
		t.Fatalf("traffic_logs multimodal columns missing: %v", err)
	}
	rows.Close()
}
