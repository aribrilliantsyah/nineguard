package proxy_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

// waitForRows polls until traffic_logs has n rows; the success path records asynchronously.
func waitForRows(t *testing.T, database *db.DB, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var c int
		_ = database.QueryRow("SELECT COUNT(*) FROM traffic_logs").Scan(&c)
		if c >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d traffic rows", n)
}

func TestProxyRecordsAPIKeyID(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "keyid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer upstream.Close()

	km := keys.NewManager(database, "")
	mm := models.NewManager(database)
	pm := providers.NewManager(database)
	tm := traffic.NewManager(database)
	if _, err := pm.CreateProvider("Mock", upstream.URL, "", "mock", true, true); err != nil {
		t.Fatal(err)
	}
	// Nothing listens on port 1, so forwarding fails with 502.
	if _, err := pm.CreateProvider("Dead", "http://127.0.0.1:1", "", "dead", false, true); err != nil {
		t.Fatal(err)
	}
	key, err := km.CreateKey("pi-dev", "custom", nil, []string{"mock/ok", "dead/x"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := proxy.NewProxy(mm, tm, km, pm, nil)
	if err != nil {
		t.Fatal(err)
	}

	send := func(model string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
			strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer "+key.RawKey)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send("mock/ok"); code != http.StatusOK {
		t.Fatalf("success path: %d", code)
	}
	if code := send("mock/other"); code != http.StatusForbidden { // model_not_allowed
		t.Fatalf("403 path: %d", code)
	}
	if code := send("dead/x"); code != http.StatusBadGateway { // upstream unreachable
		t.Fatalf("502 path: %d", code)
	}
	_ = mm.SetModelEnabled("mock/ok", false)
	if code := send("mock/ok"); code != http.StatusForbidden { // model_disabled
		t.Fatalf("firewall path: %d", code)
	}
	waitForRows(t, database, 4)

	rows, err := database.Query("SELECT status_code, api_key_id FROM traffic_logs ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var code int
		var id sql.NullString
		_ = rows.Scan(&code, &id)
		if !id.Valid || id.String != key.ID {
			t.Errorf("row status %d: api_key_id = %v, want %s", code, id, key.ID)
		}
		n++
	}
	if n != 4 {
		t.Errorf("rows = %d, want 4", n)
	}

	// 401 rows carry no key ID.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"mock/ok"}`))
	req.Header.Set("Authorization", "Bearer nope")
	p.ServeHTTP(httptest.NewRecorder(), req)
	var id sql.NullString
	_ = database.QueryRow("SELECT api_key_id FROM traffic_logs WHERE status_code = 401").Scan(&id)
	if id.Valid {
		t.Errorf("401 row has api_key_id %q", id.String)
	}
}
