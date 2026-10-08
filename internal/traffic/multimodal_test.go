package traffic_test

import (
	"testing"
	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func TestMultimodalTrafficLogAndQuery(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)

	// Record an entry with images
	err = mgr.Record(&traffic.LogEntry{
		APIKey:      "sk-ng-123",
		APIKeyName:  "image-agent",
		Model:       "gpt-4o",
		StatusCode:  200,
		ClientIP:    "127.0.0.1",
		HasImages:   true,
		ImageCount:  3,
	})
	if err != nil {
		t.Fatalf("Record entry with images: %v", err)
	}

	// Record a normal text-only entry
	err = mgr.Record(&traffic.LogEntry{
		APIKey:      "sk-ng-123",
		APIKeyName:  "text-agent",
		Model:       "gpt-4o",
		StatusCode:  200,
		ClientIP:    "127.0.0.1",
		HasImages:   false,
		ImageCount:  0,
	})
	if err != nil {
		t.Fatalf("Record text entry: %v", err)
	}

	// Query all logs
	logs, total, err := mgr.QueryLogs(traffic.FilterParams{Limit: 10})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 logs, got %d", total)
	}

	// The newest log is text-agent (HasImages: false)
	// The oldest log is image-agent (HasImages: true, ImageCount: 3)
	foundImg := false
	for _, l := range logs {
		if l.APIKeyName == "image-agent" {
			foundImg = true
			if !l.HasImages || l.ImageCount != 3 {
				t.Errorf("expected HasImages=true, ImageCount=3, got %+v", l)
			}
		}
	}
	if !foundImg {
		t.Errorf("image-agent log not found")
	}

	// Filter by HasImages = true
	hasImg := true
	imgLogs, imgTotal, err := mgr.QueryLogs(traffic.FilterParams{
		Limit:     10,
		HasImages: &hasImg,
	})
	if err != nil {
		t.Fatalf("QueryLogs with HasImages=true: %v", err)
	}
	if imgTotal != 1 || len(imgLogs) != 1 {
		t.Fatalf("expected 1 image log, got %d", imgTotal)
	}
	if imgLogs[0].APIKeyName != "image-agent" {
		t.Errorf("expected image-agent, got %s", imgLogs[0].APIKeyName)
	}
}
