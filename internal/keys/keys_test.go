package keys_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
)

func TestModelAllowedLogic(t *testing.T) {
	// Case 1: All models allowed when empty or contains *
	kAllEmpty := &keys.KeyInfo{
		AllowedModels: []string{},
	}
	if !kAllEmpty.IsAllModelsAllowed() {
		t.Errorf("expected kAllEmpty to allow all models")
	}
	if !kAllEmpty.IsModelAllowed("gpt-4o") {
		t.Errorf("expected kAllEmpty to allow gpt-4o")
	}
	if !kAllEmpty.IsModelAllowed("provider/claude-3-5-sonnet") {
		t.Errorf("expected kAllEmpty to allow provider/claude-3-5-sonnet")
	}

	kWildcard := &keys.KeyInfo{
		AllowedModels: []string{"*"},
	}
	if !kWildcard.IsAllModelsAllowed() {
		t.Errorf("expected kWildcard to allow all models")
	}
	if !kWildcard.IsModelAllowed("anything") {
		t.Errorf("expected kWildcard to allow anything")
	}

	// Case 2: Restricted models
	kRestricted := &keys.KeyInfo{
		AllowedModels: []string{
			"gpt-4o",
			"openai/o1-mini",
			"ollama/*",
			"*.flash",
		},
	}
	if kRestricted.IsAllModelsAllowed() {
		t.Errorf("expected kRestricted to NOT allow all models")
	}

	// Exact match
	if !kRestricted.IsModelAllowed("gpt-4o") {
		t.Errorf("expected gpt-4o to be allowed")
	}
	// Case-insensitive match
	if !kRestricted.IsModelAllowed("GPT-4O") {
		t.Errorf("expected GPT-4O to be allowed (case-insensitive)")
	}
	// Stripped prefix match (request has prefix, key has model)
	if !kRestricted.IsModelAllowed("someprov/gpt-4o") {
		t.Errorf("expected someprov/gpt-4o to match allowed gpt-4o")
	}
	// Direct provider model match
	if !kRestricted.IsModelAllowed("openai/o1-mini") {
		t.Errorf("expected openai/o1-mini to be allowed")
	}
	// Stripped prefix match (key has prefix, request omits prefix)
	if !kRestricted.IsModelAllowed("o1-mini") {
		t.Errorf("expected o1-mini to match allowed openai/o1-mini")
	}
	// Wildcard match
	if !kRestricted.IsModelAllowed("ollama/llama3") {
		t.Errorf("expected ollama/llama3 to match allowed ollama/*")
	}
	if !kRestricted.IsModelAllowed("gemini-1.5.flash") {
		t.Errorf("expected gemini-1.5.flash to match allowed *.flash")
	}

	// Disallowed models
	if kRestricted.IsModelAllowed("claude-3-5-sonnet") {
		t.Errorf("expected claude-3-5-sonnet to be rejected")
	}
	if kRestricted.IsModelAllowed("openrouter/deepseek-r1") {
		t.Errorf("expected openrouter/deepseek-r1 to be rejected")
	}
}

func TestSerializationAndParsing(t *testing.T) {
	models := []string{"gpt-4o", "claude-3-5-sonnet", "ollama/*"}
	serialized := keys.SerializeAllowedModels(models)

	parsed := keys.ParseAllowedModels(serialized)
	if len(parsed) != 3 {
		t.Fatalf("expected 3 parsed models, got %d", len(parsed))
	}
	if parsed[0] != "gpt-4o" || parsed[1] != "claude-3-5-sonnet" || parsed[2] != "ollama/*" {
		t.Errorf("parsed models do not match original: %v", parsed)
	}

	// Test comma separated string fallback
	csvParsed := keys.ParseAllowedModels("gpt-4o, claude-3-5-sonnet, ollama/*")
	if len(csvParsed) != 3 {
		t.Fatalf("expected 3 csv parsed models, got %d", len(csvParsed))
	}
}

func TestKeyManagerCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	mgr := keys.NewManager(database, "")

	// 1. Create a key with restricted models
	k1, err := mgr.CreateKey("Test Dev Key", []string{"gpt-4o", "claude-3-5"})
	if err != nil {
		t.Fatalf("failed to create key: %v", err)
	}
	if len(k1.AllowedModels) != 2 {
		t.Errorf("expected 2 allowed models, got %v", k1.AllowedModels)
	}

	// 2. Validate client key
	validated, ok := mgr.ValidateClientKey(k1.RawKey)
	if !ok || validated == nil {
		t.Fatalf("failed to validate client key")
	}
	if !validated.IsModelAllowed("gpt-4o") {
		t.Errorf("expected gpt-4o to be allowed")
	}
	if validated.IsModelAllowed("claude-2") {
		t.Errorf("expected claude-2 to be disallowed")
	}

	// 3. Update key
	updated, err := mgr.UpdateKey(k1.ID, "Test Dev Key Renamed", []string{"*"})
	if err != nil {
		t.Fatalf("failed to update key: %v", err)
	}
	if updated.Name != "Test Dev Key Renamed" {
		t.Errorf("expected updated name, got %s", updated.Name)
	}
	if !updated.IsAllModelsAllowed() {
		t.Errorf("expected updated key to allow all models")
	}

	// Check reloaded cache
	valAfterUpdate, ok := mgr.ValidateClientKey(k1.RawKey)
	if !ok || !valAfterUpdate.IsAllModelsAllowed() {
		t.Errorf("expected cached key to be updated with all models allowed")
	}

	// 4. List keys
	list, err := mgr.ListKeys()
	if err != nil {
		t.Fatalf("failed to list keys: %v", err)
	}
	if len(list) < 2 { // default agent key + test key
		t.Errorf("expected at least 2 keys in list, got %d", len(list))
	}
}
