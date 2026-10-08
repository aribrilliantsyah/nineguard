package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

func TestQuotaValidation(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	km := keys.NewManager(d, "")
	tm := traffic.NewManager(d)
	h := handler.New(nil, nil, tm, nil, km, nil, nil, "")

	// 1. Negative quota_limit should be rejected with 400
	payloadNeg := []byte(`{"name":"test-key","quota_limit":-100,"quota_period":"daily"}`)
	req := httptest.NewRequest("POST", "/api/v1/keys", bytes.NewReader(payloadNeg))
	rec := httptest.NewRecorder()
	h.CreateKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative quota, got %d", rec.Code)
	}

	// 2. Invalid quota_period should be rejected with 400
	payloadPeriod := []byte(`{"name":"test-key","quota_limit":1000,"quota_period":"yearly"}`)
	req = httptest.NewRequest("POST", "/api/v1/keys", bytes.NewReader(payloadPeriod))
	rec = httptest.NewRecorder()
	h.CreateKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid quota_period, got %d", rec.Code)
	}

	// 3. Valid quota should succeed with 200
	payloadValid := []byte(`{"name":"test-key","quota_limit":1000,"quota_period":"daily"}`)
	req = httptest.NewRequest("POST", "/api/v1/keys", bytes.NewReader(payloadValid))
	rec = httptest.NewRecorder()
	h.CreateKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid quota, got %d", rec.Code)
	}
	var created keys.KeyInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.QuotaLimit != 1000 || created.QuotaPeriod != "daily" {
		t.Errorf("created mismatch: %+v", created)
	}

	// 4. UpdateKey with invalid quota should return 400
	payloadUpdateInvalid := []byte(`{"name":"test-key","quota_limit":-5}`)
	req = httptest.NewRequest("POST", "/api/v1/keys/"+created.ID, bytes.NewReader(payloadUpdateInvalid))
	req.SetPathValue("id", created.ID)
	rec = httptest.NewRecorder()
	h.UpdateKey(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative quota in update, got %d", rec.Code)
	}

	// 5. UpdateKey with valid quota should update
	payloadUpdateValid := []byte(`{"name":"test-key-updated","quota_limit":5000,"quota_period":"monthly"}`)
	req = httptest.NewRequest("POST", "/api/v1/keys/"+created.ID, bytes.NewReader(payloadUpdateValid))
	req.SetPathValue("id", created.ID)
	rec = httptest.NewRecorder()
	h.UpdateKey(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid quota update, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated keys.KeyInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.QuotaLimit != 5000 || updated.QuotaPeriod != "monthly" {
		t.Errorf("updated mismatch: %+v", updated)
	}

	// 6. Traffic settings GET & POST
	req = httptest.NewRequest("GET", "/api/v1/settings/traffic", nil)
	rec = httptest.NewRecorder()
	h.GetTrafficSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GetTrafficSettings, got %d", rec.Code)
	}

	setPayload := []byte(`{"heavy_token_threshold":12000}`)
	req = httptest.NewRequest("POST", "/api/v1/settings/traffic", bytes.NewReader(setPayload))
	rec = httptest.NewRecorder()
	h.SetTrafficSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SetTrafficSettings, got %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/v1/settings/traffic", nil)
	rec = httptest.NewRecorder()
	h.GetTrafficSettings(rec, req)
	var settingsRes map[string]int
	_ = json.Unmarshal(rec.Body.Bytes(), &settingsRes)
	if settingsRes["heavy_token_threshold"] != 12000 {
		t.Errorf("expected 12000 threshold, got %d", settingsRes["heavy_token_threshold"])
	}
}
