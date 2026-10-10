package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClearRemovedModelsAndGroupCleanup(t *testing.T) {
	h, database := setupTestHandler(t)
	defer database.Close()

	for _, q := range []string{
		`INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('p/gone1', 'p/gone1', 'p', 1, CURRENT_TIMESTAMP)`,
		`INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('p/gone2', 'p/gone2', 'p', 1, CURRENT_TIMESTAMP)`,
		`INSERT INTO models (id, name, provider_id, enabled) VALUES ('p/live', 'p/live', 'p', 1)`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	grp, err := h.models.CreateGroup("G", "", []string{"p/live", "p/gone1", "p/*"})
	if err != nil {
		t.Fatal(err)
	}

	// Group endpoint: reports unavailable entries.
	rec := httptest.NewRecorder()
	h.ListModelGroups(rec, httptest.NewRequest(http.MethodGet, "/api/v1/model-groups", nil))
	var list struct {
		Groups []struct {
			UnavailableCount  int      `json:"unavailable_count"`
			UnavailableModels []string `json:"unavailable_models"`
			ModelsCount       int      `json:"models_count"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Groups) != 1 {
		t.Fatalf("list groups: %v %s", err, rec.Body.String())
	}
	if g := list.Groups[0]; g.UnavailableCount != 1 || g.ModelsCount != 3 || g.UnavailableModels[0] != "p/gone1" {
		t.Fatalf("group payload wrong: %+v", g)
	}

	// Remove unavailable from the group.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/model-groups/"+grp.ID+"/remove-unavailable", strings.NewReader("{}"))
	req.SetPathValue("id", grp.ID)
	rec = httptest.NewRecorder()
	h.RemoveUnavailableFromGroup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove-unavailable: %d %s", rec.Code, rec.Body.String())
	}
	var removeRes struct {
		Removed int `json:"removed"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &removeRes)
	if removeRes.Removed != 1 {
		t.Fatalf("removed = %d, want 1", removeRes.Removed)
	}

	// Unknown group is a 400, not a panic.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/model-groups/nope/remove-unavailable", strings.NewReader("{}"))
	req.SetPathValue("id", "nope")
	rec = httptest.NewRecorder()
	h.RemoveUnavailableFromGroup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown group: %d", rec.Code)
	}

	// Clear removed: hard-deletes only Removed Models.
	rec = httptest.NewRecorder()
	h.ClearRemovedModels(rec, httptest.NewRequest(http.MethodPost, "/api/v1/models/clear-removed", strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear-removed: %d %s", rec.Code, rec.Body.String())
	}
	var clearRes struct {
		Deleted int `json:"deleted"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &clearRes)
	if clearRes.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", clearRes.Deleted)
	}
	var left int
	if err := database.QueryRow(`SELECT COUNT(*) FROM models`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("models left = %d (err %v), want only p/live", left, err)
	}

	// Second call is a no-op.
	rec = httptest.NewRecorder()
	h.ClearRemovedModels(rec, httptest.NewRequest(http.MethodPost, "/api/v1/models/clear-removed", strings.NewReader("{}")))
	_ = json.Unmarshal(rec.Body.Bytes(), &clearRes)
	if clearRes.Deleted != 0 {
		t.Fatalf("second clear deleted %d, want 0", clearRes.Deleted)
	}
}

func TestListModelsExposesRemovedFlag(t *testing.T) {
	h, database := setupTestHandler(t)
	defer database.Close()

	if _, err := database.Exec(`INSERT INTO models (id, name, provider_id, enabled, removed_at) VALUES ('p/gone', 'p/gone', 'p', 1, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ListModels(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models", nil))
	var out []struct {
		ID        string  `json:"id"`
		Removed   bool    `json:"removed"`
		RemovedAt *string `json:"removed_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 1 {
		t.Fatalf("decode: %v %s", err, rec.Body.String())
	}
	if !out[0].Removed || out[0].RemovedAt == nil {
		t.Fatalf("flag missing: %+v", out[0])
	}
}
