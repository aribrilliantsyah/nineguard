package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
)

func TestListKeysLegacyAndPaged(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	km := keys.NewManager(database, "") // creates "Default Agent Key"
	for _, n := range []string{"pi-dev", "marinara"} {
		if _, err := km.CreateKey(n, "all", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	h := handler.New(nil, nil, nil, nil, km, nil, nil, "")

	get := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys"+query, nil)
		w := httptest.NewRecorder()
		h.ListKeys(w, req)
		return w
	}

	// Legacy: no page -> {"keys": [...]} only, all keys.
	w := get("")
	var legacy map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &legacy)
	if _, hasTotal := legacy["total"]; hasTotal || w.Code != http.StatusOK {
		t.Errorf("legacy response changed: %d %s", w.Code, w.Body.String())
	}
	var all []keys.KeyInfo
	_ = json.Unmarshal(legacy["keys"], &all)
	if len(all) != 3 {
		t.Errorf("legacy keys = %d, want 3", len(all))
	}

	// Paged.
	w = get("?page=1&limit=10&sort=name&order=asc&q=a")
	if w.Code != http.StatusOK {
		t.Fatalf("paged: %d %s", w.Code, w.Body.String())
	}
	var pg keys.KeyPage
	_ = json.Unmarshal(w.Body.Bytes(), &pg)
	if pg.Total != 2 || pg.Page != 1 || pg.Limit != 10 || len(pg.Keys) != 2 || pg.Keys[0].Name != "Default Agent Key" || pg.Keys[1].Name != "marinara" {
		t.Errorf("paged = %+v", pg)
	}

	// Limit clamped.
	w = get("?page=1&limit=7")
	_ = json.Unmarshal(w.Body.Bytes(), &pg)
	if pg.Limit != 10 {
		t.Errorf("limit 7 clamped to %d, want 10", pg.Limit)
	}

	// Invalid params -> 400 listing allowed values.
	for _, q := range []string{"?page=0", "?page=x", "?page=1&limit=x", "?page=1&sort=bogus", "?page=1&order=up", "?page=1&status=x", "?page=1&mode=x"} {
		w = get(q)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, w.Code)
		}
	}
	w = get("?page=1&sort=bogus")
	if !strings.Contains(w.Body.String(), "last_active") {
		t.Errorf("400 body should list allowed sorts: %s", w.Body.String())
	}
}
