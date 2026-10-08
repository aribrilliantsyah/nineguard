package traffic_test

import (
	"bytes"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func TestPayloadStorageAndPurge(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)

	// Insert parent traffic row
	res, err := d.Exec(`INSERT INTO traffic_logs (model, status_code, timestamp) VALUES ('gpt-4o', 200, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert traffic: %v", err)
	}
	trafficID, _ := res.LastInsertId()

	reqBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	respBody := []byte(`{"choices":[{"message":{"role":"assistant","content":"world"}}]}`)

	// 1. Save payload
	err = mgr.SavePayload(trafficID, reqBody, respBody)
	if err != nil {
		t.Fatalf("SavePayload: %v", err)
	}

	// 2. Retrieve payload
	p, err := mgr.GetPayload(trafficID)
	if err != nil {
		t.Fatalf("GetPayload: %v", err)
	}
	if p.RequestBody != string(reqBody) {
		t.Errorf("req body mismatch, got: %s", p.RequestBody)
	}
	if p.ResponseBody != string(respBody) {
		t.Errorf("resp body mismatch, got: %s", p.ResponseBody)
	}

	// 3. Test 512KB clamping
	hugeBody := bytes.Repeat([]byte("A"), 600*1024) // 600KB
	err = mgr.SavePayload(trafficID, hugeBody, respBody)
	if err != nil {
		t.Fatalf("SavePayload huge: %v", err)
	}
	pClamped, err := mgr.GetPayload(trafficID)
	if err != nil {
		t.Fatalf("GetPayload clamped: %v", err)
	}
	if len(pClamped.RequestBody) > 512*1024 {
		t.Errorf("expected clamped payload <= 512KB, got %d bytes", len(pClamped.RequestBody))
	}

	// 4. Test Purge
	// Backdate payload creation
	_, _ = d.Exec(`UPDATE traffic_payloads SET created_at = datetime('now', '-8 days') WHERE traffic_id = ?`, trafficID)
	deleted, err := mgr.PurgeOldPayloads(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("PurgeOldPayloads: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted row, got %d", deleted)
	}

	_, err = mgr.GetPayload(trafficID)
	if err == nil {
		t.Errorf("expected error after purge, got nil")
	}
}
