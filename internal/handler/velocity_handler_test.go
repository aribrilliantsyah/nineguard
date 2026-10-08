package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

func TestKeyVelocityHandler(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	km := keys.NewManager(d, "")
	tm := traffic.NewManager(d)
	h := handler.New(nil, nil, tm, nil, km, nil, nil, "")

	k, err := km.CreateKey("velocity-agent", "all", nil, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	// Insert past traffic
	_, _ = d.Exec(`INSERT INTO traffic_logs (api_key_id, model, status_code, total_tokens, timestamp)
		VALUES (?, 'gpt-4o', 200, 15000, datetime('now', '-2 hours'))`, k.ID)

	req := httptest.NewRequest("GET", "/api/v1/keys/"+k.ID+"/velocity", nil)
	req.SetPathValue("id", k.ID)
	rec := httptest.NewRecorder()

	h.GetKeyVelocity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var res traffic.VelocityStats
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if res.Tokens24h != 15000 {
		t.Errorf("expected 15000 tokens_24h, got %d", res.Tokens24h)
	}
}
