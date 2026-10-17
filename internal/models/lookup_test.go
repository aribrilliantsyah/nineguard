package models_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/models"
)

func TestLookupStatus(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	_, _ = d.Exec(`INSERT INTO models (id,name,provider_id,enabled) VALUES ('p/on','p/on','p',1),('p/off','p/off','p',0)`)
	_, _ = d.Exec(`INSERT INTO models (id,name,provider_id,enabled,removed_at) VALUES ('p/gone','p/gone','p',1,CURRENT_TIMESTAMP)`)

	f, e, r := models.LookupStatus(d, "P/ON")
	if !f || !e || r {
		t.Errorf("on: %v %v %v", f, e, r)
	}
	f, e, r = models.LookupStatus(d, "p/off")
	if !f || e || r {
		t.Errorf("off: %v %v %v", f, e, r)
	}
	f, _, r = models.LookupStatus(d, "p/gone")
	if !f || !r {
		t.Errorf("gone: %v %v", f, r)
	}
	if f, _, _ = models.LookupStatus(d, "p/unknown"); f {
		t.Error("unknown must be not found")
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM models WHERE id='p/unknown'`).Scan(&n)
	if n != 0 {
		t.Error("LookupStatus must not auto-register")
	}
}

func TestIsModelEnabledNeverRegisters(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m := models.NewManager(d)
	if !m.IsModelEnabled("cx/unknown") {
		t.Fatal("unknown model must not read as disabled")
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM models`).Scan(&n)
	if n != 0 {
		t.Fatalf("IsModelEnabled registered %d rows", n)
	}
}

func TestListModelsMergesTrafficByModelID(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, q := range []string{
		`INSERT INTO models (id, name, provider_id, enabled) VALUES ('9dak/cx/x','9dak/cx/x','9dak',1)`,
		`INSERT INTO traffic_logs (model, model_id, status_code, total_tokens) VALUES ('cx/x', '9dak/cx/x', 200, 10)`,
		`INSERT INTO traffic_logs (model, model_id, status_code, total_tokens) VALUES ('9dak/cx/x', '9dak/cx/x', 200, 20)`,
		`INSERT INTO traffic_logs (model, status_code, total_tokens) VALUES ('9dak/cx/x', 200, 5)`, // pre-ADR 0008 row, no model_id
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	list, err := models.NewManager(d).ListModels("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].TotalRequests != 3 || list[0].TotalTokens != 35 {
		t.Fatalf("want 1 row with 3 requests / 35 tokens, got %+v", list)
	}
}
