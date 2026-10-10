package proxy_test

import (
	"encoding/json"
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

// removedModelSetup starts a stub upstream that answers every chat request with the
// given status and body, and marks mock/gone as a Removed Model.
func removedModelSetup(t *testing.T, upstreamStatus int, upstreamBody string) (*proxy.Proxy, *traffic.Manager, string) {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "removed.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstreamStatus)
		_, _ = w.Write([]byte(upstreamBody))
	}))
	t.Cleanup(upstream.Close)

	keysMgr := keys.NewManager(database, "")
	modelsMgr := models.NewManager(database)
	providersMgr := providers.NewManager(database)
	trafficMgr := traffic.NewManager(database)

	if _, err := providersMgr.CreateProvider("Mock", upstream.URL, "mock-upstream-key", "mock", true, true); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('mock/gone', 'mock/gone', 'mock', 1, CURRENT_TIMESTAMP)`,
		`INSERT INTO models (id, name, provider_id, enabled) VALUES ('mock/live', 'mock/live', 'mock', 1)`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	key, err := keysMgr.CreateKey("agent", "all", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keysMgr, providersMgr, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p, trafficMgr, key.RawKey
}

func chat(t *testing.T, p *proxy.Proxy, rawKey, model string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

// lastLog waits for the async traffic write and returns the newest row.
func lastLog(t *testing.T, tm *traffic.Manager) traffic.LogEntry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		logs, _, err := tm.QueryLogs(traffic.FilterParams{Limit: 5})
		if err == nil && len(logs) > 0 {
			return logs[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no traffic row recorded")
	return traffic.LogEntry{}
}

func TestRemovedModelUpstream404IsRewritten(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest} {
		p, tm, key := removedModelSetup(t, status, `{"error":{"message":"The model does not exist"}}`)
		rec := chat(t, p, key, "mock/gone")

		if rec.Code != http.StatusNotFound {
			t.Fatalf("upstream %d: want 404, got %d: %s", status, rec.Code, rec.Body.String())
		}
		var out struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body not json: %v: %s", err, rec.Body.String())
		}
		if out.Error.Code != "model_removed" || !strings.Contains(out.Error.Message, "mock/gone") {
			t.Fatalf("upstream %d: bad error %+v", status, out.Error)
		}

		entry := lastLog(t, tm)
		if entry.StatusCode != http.StatusNotFound {
			t.Errorf("upstream %d: logged status %d, want 404", status, entry.StatusCode)
		}
		if entry.ErrorMessage == nil || !strings.Contains(*entry.ErrorMessage, "removed from its provider") {
			t.Errorf("upstream %d: traffic row lacks the removed message: %v", status, entry.ErrorMessage)
		}
	}
}

func TestRemovedModelOtherUpstreamStatusesPassThrough(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		p, _, key := removedModelSetup(t, status, `{"error":{"message":"upstream said no"}}`)
		rec := chat(t, p, key, "mock/gone")
		if rec.Code != status {
			t.Errorf("upstream %d: status changed to %d", status, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "upstream said no") {
			t.Errorf("upstream %d: body was rewritten: %s", status, rec.Body.String())
		}
	}
}

func TestRemovedModelStillReachesUpstreamWhenItWorks(t *testing.T) {
	// No pre-block: a model flagged removed that upstream serves again must still work.
	p, _, key := removedModelSetup(t, http.StatusOK, `{"id":"x","usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	rec := chat(t, p, key, "mock/gone")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLiveModelUpstream404IsNotRewritten(t *testing.T) {
	p, _, key := removedModelSetup(t, http.StatusNotFound, `{"error":{"message":"genuine upstream 404"}}`)
	rec := chat(t, p, key, "mock/live")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "genuine upstream 404") {
		t.Fatalf("live model error must pass through, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "model_removed") {
		t.Fatal("live model must not get model_removed")
	}
}
