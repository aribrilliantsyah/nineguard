package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/providers"
)

// Anthropic's API authenticates only via x-api-key and rejects Bearer tokens.
func anthropicLikeUpstream(t *testing.T, key string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-5","type":"model"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProviderProbeAndSyncSendXAPIKey(t *testing.T) {
	const key = "sk-ant-test-fake"
	srv := anthropicLikeUpstream(t, key)

	d, err := db.InitDB(filepath.Join(t.TempDir(), "providers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	mgr := providers.NewManager(d)

	res, err := mgr.TestProvider(context.Background(), srv.URL, key)
	if err != nil {
		t.Fatal(err)
	}
	if res["ok"] != true || res["models_count"] != 1 {
		t.Fatalf("probe = %v, want ok with 1 model", res)
	}

	if _, err := mgr.CreateProvider("Anthropic", srv.URL, key, "anthropic", false, true); err != nil {
		t.Fatal(err)
	}
	models, err := mgr.AggregateModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0]["id"] != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("models = %v, want [anthropic/claude-sonnet-4-5]", models)
	}
}
