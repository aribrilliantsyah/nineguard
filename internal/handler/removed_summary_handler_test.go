package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemovedModelsSummaryEndpoint(t *testing.T) {
	h, database := setupTestHandler(t)
	defer database.Close()

	// Nothing removed: empty arrays, never null, so the dashboard can iterate safely.
	rec := httptest.NewRecorder()
	h.RemovedModelsSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models/removed-summary", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"providers", "groups"} {
		if string(raw[k]) != "[]" {
			t.Errorf("%s = %s, want []", k, raw[k])
		}
	}
	var syncStatus struct {
		PendingProviders []string `json:"pending_providers"`
	}
	if err := json.Unmarshal(raw["sync"], &syncStatus); err != nil || syncStatus.PendingProviders == nil {
		t.Errorf("sync.pending_providers must be [] not null: %s", raw["sync"])
	}

	// With a removed model and a group holding it.
	if _, err := database.Exec(`INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('p/gone', 'p/gone', 'p', 1, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.models.CreateGroup("G", "", []string{"p/gone"}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.RemovedModelsSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models/removed-summary", nil))
	var out struct {
		RemovedCount     int `json:"removed_count"`
		UnavailableCount int `json:"unavailable_count"`
		Groups           []struct {
			Name string `json:"name"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.RemovedCount != 1 || out.UnavailableCount != 1 || len(out.Groups) != 1 || out.Groups[0].Name != "G" {
		t.Fatalf("summary wrong: %s", rec.Body.String())
	}
}
