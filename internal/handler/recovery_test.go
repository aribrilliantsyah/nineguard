package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/handler"
)

func TestHandlerRecoveryFlow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_handler_recovery.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	authMgr := auth.NewManager(database, true)
	user, _, err := authMgr.Setup("admin", "admin123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	h := handler.New(authMgr, nil, nil, nil, nil, nil, nil, "http://localhost:8080")

	// 1. Get profile (should have has_recovery = false)
	req := httptest.NewRequest("GET", "/api/v1/profile", nil)
	ctx := auth.ContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.GetProfile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetProfile returned status %d: %s", w.Code, w.Body.String())
	}
	var profResp struct {
		User auth.User `json:"user"`
	}
	if err := json.NewDecoder(w.Body).Decode(&profResp); err != nil {
		t.Fatalf("Failed to decode profile response: %v", err)
	}
	if profResp.User.HasRecovery {
		t.Fatalf("Expected HasRecovery to be false initially")
	}

	// 2. Set recovery question with wrong password
	body, _ := json.Marshal(map[string]string{
		"question":         "What is your childhood nickname?",
		"answer":           "Ace",
		"current_password": "wrongpassword",
	})
	req = httptest.NewRequest("POST", "/api/v1/profile/recovery", bytes.NewReader(body))
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.SetRecoveryQuestion(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 with wrong current password, got %d", w.Code)
	}

	// 3. Set recovery question with correct password
	body, _ = json.Marshal(map[string]string{
		"question":         "What is your childhood nickname?",
		"answer":           "Ace",
		"current_password": "admin123",
	})
	req = httptest.NewRequest("POST", "/api/v1/profile/recovery", bytes.NewReader(body))
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.SetRecoveryQuestion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 setting recovery question, got %d: %s", w.Code, w.Body.String())
	}

	// 4. Get recovery question via public endpoint
	req = httptest.NewRequest("GET", "/api/v1/auth/recovery?username=admin", nil)
	w = httptest.NewRecorder()
	h.GetRecoveryQuestion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 getting recovery question, got %d: %s", w.Code, w.Body.String())
	}
	var recQResp struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(w.Body).Decode(&recQResp); err != nil {
		t.Fatalf("Failed to decode recovery question: %v", err)
	}
	if recQResp.Question != "What is your childhood nickname?" {
		t.Fatalf("Unexpected recovery question: %s", recQResp.Question)
	}

	// 5. Recover password with wrong answer
	body, _ = json.Marshal(map[string]string{
		"username":     "admin",
		"answer":       "Wrong",
		"new_password": "newpassword123",
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/recovery", bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.RecoverPassword(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 recovering with wrong answer, got %d", w.Code)
	}

	// 6. Recover password with correct answer
	body, _ = json.Marshal(map[string]string{
		"username":     "admin",
		"answer":       "ace",
		"new_password": "newpassword123",
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/recovery", bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.RecoverPassword(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 recovering with correct answer, got %d: %s", w.Code, w.Body.String())
	}

	// 7. Verify login works with new password
	_, _, err = authMgr.Login("admin", "newpassword123")
	if err != nil {
		t.Fatalf("Failed to log in with new password after recovery: %v", err)
	}
}
