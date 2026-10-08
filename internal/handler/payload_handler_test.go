package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/traffic"
)

func TestPayloadHandler(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	tm := traffic.NewManager(d)
	h := handler.New(nil, nil, tm, nil, nil, nil, nil, "")

	// Insert dummy traffic
	res, _ := d.Exec(`INSERT INTO traffic_logs (model, status_code, timestamp) VALUES ('gpt-4o', 200, datetime('now'))`)
	trafficID, _ := res.LastInsertId()

	// 1. Not found initially
	req := httptest.NewRequest("GET", "/api/v1/traffic/1/payload", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.GetTrafficPayload(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// 2. Save payload and test 200
	_ = tm.SavePayload(trafficID, []byte(`{"req":"test"}`), []byte(`{"resp":"ok"}`))

	req = httptest.NewRequest("GET", "/api/v1/traffic/1/payload", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	h.GetTrafficPayload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var p traffic.PayloadEntry
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.RequestBody != `{"req":"test"}` {
		t.Errorf("expected request_body, got %s", p.RequestBody)
	}

	// 3. Update record_payloads setting
	body := []byte(`{"record_payloads":"all","heavy_token_threshold":10000}`)
	req = httptest.NewRequest("POST", "/api/v1/settings/traffic", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	h.SetTrafficSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/v1/settings/traffic", nil)
	rec = httptest.NewRecorder()
	h.GetTrafficSettings(rec, req)
	var sMap map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &sMap)
	if sMap["record_payloads"] != "all" {
		t.Errorf("expected record_payloads 'all', got %v", sMap["record_payloads"])
	}
}
