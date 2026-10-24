package traffic_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func rec(t *testing.T, tm *traffic.Manager, keyID, model, modelID string, tokens int) {
	t.Helper()
	if err := tm.Record(&traffic.LogEntry{APIKey: "sk-ng-test-key-1234", APIKeyID: keyID, Model: model, ModelID: modelID, StatusCode: 200, TotalTokens: tokens}); err != nil {
		t.Fatal(err)
	}
}

func TestModelQuotaUsageAndVelocity(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	tm := traffic.NewManager(d)
	now := time.Now()

	rec(t, tm, "k1", "a", "p/a", 100)
	rec(t, tm, "k1", "a", "P/A", 50) // case-insensitive
	rec(t, tm, "k1", "b", "p/b", 7)
	rec(t, tm, "k2", "a", "p/a", 999) // other key
	rec(t, tm, "k1", "gpt", "", 1)    // no model_id -> NULL
	_, _ = d.Exec(`INSERT INTO traffic_logs (api_key_id,model,model_id,status_code,total_tokens,timestamp) VALUES ('k1','a','p/a',200,500,datetime('now','-3 days'))`)

	got, reset, err := tm.GetModelQuotaUsage("k1", "p/a", "daily", traffic.ResetRule{}, now)
	if err != nil || got != 150 || !reset.After(now) {
		t.Fatalf("daily: %d %v %v", got, reset, err)
	}
	if got, _, _ = tm.GetModelQuotaUsage("k1", "p/a", "total", traffic.ResetRule{}, now); got != 650 {
		t.Fatalf("total: %d", got)
	}
	m, err := traffic.GetModelUsageSince(d, "k1", now.Add(-time.Hour))
	if err != nil || m["p/a"] != 150 || m["p/b"] != 7 || len(m) != 2 {
		t.Fatalf("usage map: %v %v", m, err)
	}

	_, _ = d.Exec(`INSERT INTO traffic_logs (api_key_id,model,status_code,total_tokens,timestamp) VALUES ('k1','gpt-4o',200,40,datetime('now','-1 hours'))`)
	v, err := tm.GetModelVelocityStats("k1", "9router/gpt-4o", "gpt-4o", now)
	if err != nil || v.Tokens24h != 40 {
		t.Fatalf("velocity legacy: %+v %v", v, err)
	}
	v, _ = tm.GetModelVelocityStats("k1", "p/a", "", now)
	if v.Tokens24h != 150 {
		t.Fatalf("velocity: %+v", v)
	}
}

func TestQuotaWarnPercent(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	tm := traffic.NewManager(d)
	if tm.GetQuotaWarnPercent() != 80 {
		t.Fatal("default 80")
	}
	if tm.SetQuotaWarnPercent(0) == nil || tm.SetQuotaWarnPercent(101) == nil {
		t.Fatal("0 and 101 must fail")
	}
	if err := tm.SetQuotaWarnPercent(90); err != nil || tm.GetQuotaWarnPercent() != 90 {
		t.Fatal("set 90")
	}
}
