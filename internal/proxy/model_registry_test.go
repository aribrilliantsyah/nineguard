package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

// Bug 1 (docs/BUGS_2026-10-16.md): a request without the provider prefix must resolve to the
// canonical Model ID, share its firewall and statistics, and never create registry rows.
// Unknown names must be rejected instead of registered and forwarded.

const (
	regCanonical = "9dak/cx/gpt-5.6-luna"
	regBare      = "cx/gpt-5.6-luna"
	regRemoved   = "9dak/cc/claude-fable-5-1"
)

type regEnv struct {
	p   *proxy.Proxy
	d   *db.DB
	km  *keys.Manager
	mm  *models.Manager
	all string // raw key, model access "all"

	mu       sync.Mutex
	upstream []string // model field of every body the stub upstream received
}

func newRegEnv(t *testing.T) *regEnv {
	t.Helper()
	d, err := db.InitDB(filepath.Join(t.TempDir(), "reg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	e := &regEnv{d: d}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(b, &body)
		e.mu.Lock()
		e.upstream = append(e.upstream, body.Model)
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	t.Cleanup(up.Close)

	e.km = keys.NewManager(d, "")
	pm := providers.NewManager(d)
	if _, err := pm.CreateProvider("9router DAK", up.URL, "", "9dak", true, true); err != nil {
		t.Fatal(err)
	}
	e.mm = models.NewManager(d)
	for _, q := range []string{
		`INSERT INTO models (id, name, provider_id, enabled) VALUES ('` + regCanonical + `', '` + regCanonical + `', '9dak', 1)`,
		`INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('` + regRemoved + `', '` + regRemoved + `', '9dak', 1, CURRENT_TIMESTAMP)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	k, err := e.km.CreateKey("agent-all", "all", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.all = k.RawKey

	e.p, err = proxy.NewProxy(e.mm, traffic.NewManager(d), e.km, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *regEnv) send(t *testing.T, rawKey, model string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	e.p.ServeHTTP(rec, req)
	return rec
}

func (e *regEnv) upstreamModels() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.upstream...)
}

func (e *regEnv) modelCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.d.QueryRow(`SELECT COUNT(*) FROM models`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *regEnv) rowExists(t *testing.T, id string) bool {
	t.Helper()
	var n int
	if err := e.d.QueryRow(`SELECT COUNT(*) FROM models WHERE id = ? COLLATE NOCASE`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// Registry must stay exactly as the sync left it (2 seeded rows).
func (e *regEnv) assertRegistryUntouched(t *testing.T) {
	t.Helper()
	if n := e.modelCount(t); n != 2 {
		rows, _ := e.d.Query(`SELECT id, provider_id FROM models ORDER BY id`)
		var got []string
		for rows.Next() {
			var id, prov string
			_ = rows.Scan(&id, &prov)
			got = append(got, id+" (provider="+prov+")")
		}
		rows.Close()
		t.Fatalf("registry changed by a request: want 2 rows, got %d: %v", n, got)
	}
}

func (e *regEnv) waitTrafficModelID(t *testing.T, want string, wantRows int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var got []string
	for time.Now().Before(deadline) {
		got = got[:0]
		rows, err := e.d.Query(`SELECT COALESCE(model_id,'') FROM traffic_logs WHERE status_code = 200 ORDER BY id`)
		if err == nil {
			for rows.Next() {
				var s string
				_ = rows.Scan(&s)
				got = append(got, s)
			}
			rows.Close()
		}
		if len(got) >= wantRows {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(got) != wantRows {
		t.Fatalf("want %d logged 200 rows, got %d (%v)", wantRows, len(got), got)
	}
	for _, id := range got {
		if id != want {
			t.Fatalf("traffic model_id: want %q on every row, got %v", want, got)
		}
	}
}

func TestBareSubProviderModelCreatesNoGhostRow(t *testing.T) {
	e := newRegEnv(t)
	rec := e.send(t, e.all, regBare)
	if rec.Code != http.StatusOK {
		t.Fatalf("bare request must still be served, got %d: %s", rec.Code, rec.Body.String())
	}
	if up := e.upstreamModels(); len(up) != 1 || up[0] != regBare {
		t.Fatalf("upstream must receive the bare 9router name %q, got %v", regBare, up)
	}
	if e.rowExists(t, regBare) {
		t.Fatalf("ghost row %q was auto-registered", regBare)
	}
	e.assertRegistryUntouched(t)
}

func TestPrefixedRequestStillWorksAndAddsNoRow(t *testing.T) {
	e := newRegEnv(t)
	rec := e.send(t, e.all, regCanonical)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if up := e.upstreamModels(); len(up) != 1 || up[0] != regBare {
		t.Fatalf("provider prefix must be stripped for upstream (%q), got %v", regBare, up)
	}
	e.assertRegistryUntouched(t)
}

func TestUnknownModelIsRejectedOnAllKey(t *testing.T) {
	for _, name := range []string{
		"cc/zz-probe-opus",      // invented sub-provider model
		"a9dak/cc/opus99",       // typo of the provider prefix
		"zz-probe/cc/opus-55",   // unknown provider
		"9dak/cx/does-not-exist", // right prefix, model not in listing
	} {
		t.Run(name, func(t *testing.T) {
			e := newRegEnv(t)
			rec := e.send(t, e.all, name)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("want 404, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "model_not_found") {
				t.Fatalf("want error code model_not_found, got: %s", rec.Body.String())
			}
			if up := e.upstreamModels(); len(up) != 0 {
				t.Fatalf("unknown model must not reach upstream, got %v", up)
			}
			e.assertRegistryUntouched(t)
		})
	}
}

func TestUnknownModelIsRejectedOnCustomKey(t *testing.T) {
	e := newRegEnv(t)
	k, err := e.km.CreateKey("agent-custom", "custom", nil, []string{"9dak/*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cc/zz-probe-opus", "a9dak/cc/opus99", "9dak/cx/does-not-exist"} {
		rec := e.send(t, k.RawKey, name)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Fatalf("%s: want 404/403, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	if up := e.upstreamModels(); len(up) != 0 {
		t.Fatalf("unknown models must not reach upstream, got %v", up)
	}
	e.assertRegistryUntouched(t)
}

func TestBareNameIsAllowedForKeyThatAllowsCanonicalName(t *testing.T) {
	e := newRegEnv(t)
	k, err := e.km.CreateKey("agent-custom", "custom", nil, []string{"9dak/*"})
	if err != nil {
		t.Fatal(err)
	}
	rec := e.send(t, k.RawKey, regBare)
	if rec.Code != http.StatusOK {
		t.Fatalf("bare name is the same model as %s, want 200, got %d: %s", regCanonical, rec.Code, rec.Body.String())
	}
	e.assertRegistryUntouched(t)
}

func TestDisabledModelCannotBeBypassedWithBareName(t *testing.T) {
	e := newRegEnv(t)
	if err := e.mm.SetModelEnabled(regCanonical, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{regCanonical, regBare} {
		rec := e.send(t, e.all, name)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "model_disabled") {
			t.Fatalf("%s: want 403 model_disabled, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	if up := e.upstreamModels(); len(up) != 0 {
		t.Fatalf("disabled model must not reach upstream, got %v", up)
	}
	if e.rowExists(t, regBare) {
		t.Fatalf("ghost row %q was auto-registered", regBare)
	}
}

func TestBareAndPrefixedShareOneTrafficModelID(t *testing.T) {
	e := newRegEnv(t)
	for _, name := range []string{regBare, regCanonical} {
		if rec := e.send(t, e.all, name); rec.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	e.waitTrafficModelID(t, regCanonical, 2)

	// The Models page (ListModels) must count both requests under the one canonical row.
	deadline := time.Now().Add(3 * time.Second)
	var reqs int
	for time.Now().Before(deadline) {
		list, err := e.mm.ListModels("")
		if err != nil {
			t.Fatal(err)
		}
		for _, mi := range list {
			if mi.ID == regCanonical {
				reqs = mi.TotalRequests
			}
		}
		if reqs == 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Models page shows %d requests for %s, want 2 (bare and prefixed merged)", reqs, regCanonical)
}

func TestRemovedModelIsStillKnown(t *testing.T) {
	// ADR 0006: a soft-removed model keeps working until upstream says otherwise.
	e := newRegEnv(t)
	rec := e.send(t, e.all, regRemoved)
	if rec.Code != http.StatusOK {
		t.Fatalf("removed model must still reach upstream, got %d: %s", rec.Code, rec.Body.String())
	}
	e.assertRegistryUntouched(t)
}

func TestEmptyModelNameRegistersNothing(t *testing.T) {
	e := newRegEnv(t)
	e.send(t, e.all, "")
	e.assertRegistryUntouched(t)
}

func TestProviderWithoutListingIsNotStrict(t *testing.T) {
	// A provider with no registered models has no usable listing, so nothing can be judged unknown.
	e := newRegEnv(t)
	if _, err := e.d.Exec(`DELETE FROM models`); err != nil {
		t.Fatal(err)
	}
	rec := e.send(t, e.all, "cc/anything")
	if rec.Code != http.StatusOK {
		t.Fatalf("provider without listing must pass requests through, got %d: %s", rec.Code, rec.Body.String())
	}
	if n := e.modelCount(t); n != 0 {
		t.Fatalf("request must not register a row, got %d rows", n)
	}
}

func TestModelLookupIsCaseInsensitive(t *testing.T) {
	e := newRegEnv(t)
	rec := e.send(t, e.all, "9dak/cx/GPT-5.6-LUNA")
	if rec.Code == http.StatusNotFound {
		t.Fatalf("case difference must not make a known model unknown: %s", rec.Body.String())
	}
}

func TestUnknownModelIsLoggedAs404(t *testing.T) {
	e := newRegEnv(t)
	e.send(t, e.all, "cc/zz-probe-opus")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		_ = e.d.QueryRow(`SELECT COUNT(*) FROM traffic_logs WHERE status_code = 404 AND model = 'cc/zz-probe-opus'`).Scan(&n)
		if n == 1 {
			e.assertRegistryUntouched(t)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("rejected request was not logged as 404")
}
