package auth_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"nineguard/internal/auth"
	"nineguard/internal/db"
)

func TestAuthRecovery(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_auth.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	mgr := auth.NewManager(database, true)

	// Setup initial admin
	user, _, err := mgr.Setup("admin", "secret123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if user.HasRecovery {
		t.Fatalf("Expected HasRecovery to be false initially")
	}

	// Login
	user, token, err := mgr.Login("admin", "secret123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if user.HasRecovery {
		t.Fatalf("Expected HasRecovery to be false on login")
	}

	// ValidateSession
	sessUser, err := mgr.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if sessUser.HasRecovery {
		t.Fatalf("Expected session user HasRecovery false")
	}

	// GetRecoveryQuestion when not set
	_, err = mgr.GetRecoveryQuestion("admin")
	if err == nil {
		t.Fatalf("Expected error getting recovery question before set")
	}

	// SetRecoveryQuestion
	q := "What is the name of your first pet?"
	ans := "Fluffy"
	if err := mgr.SetRecoveryQuestion(user.ID, q, ans); err != nil {
		t.Fatalf("SetRecoveryQuestion failed: %v", err)
	}

	// GetRecoveryQuestion
	gotQ, err := mgr.GetRecoveryQuestion("admin")
	if err != nil {
		t.Fatalf("GetRecoveryQuestion failed: %v", err)
	}
	if gotQ != q {
		t.Fatalf("Expected question %q, got %q", q, gotQ)
	}

	// Login again to check HasRecovery is true
	user, _, err = mgr.Login("admin", "secret123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !user.HasRecovery {
		t.Fatalf("Expected HasRecovery to be true after set")
	}
	if user.RecoveryQuestion != q {
		t.Fatalf("Expected RecoveryQuestion %q, got %q", q, user.RecoveryQuestion)
	}

	// RecoverPassword with wrong answer
	err = mgr.RecoverPassword("admin", "WrongAnswer", "newpass123")
	if err == nil {
		t.Fatalf("Expected RecoverPassword to fail with wrong answer")
	}

	// RecoverPassword with correct answer (case-insensitive)
	err = mgr.RecoverPassword("admin", "  fluffy  ", "newpass123")
	if err != nil {
		t.Fatalf("RecoverPassword failed: %v", err)
	}

	// Old password should no longer work
	_, _, err = mgr.Login("admin", "secret123")
	if err == nil {
		t.Fatalf("Expected old password to fail")
	}

	// New password should work
	user, _, err = mgr.Login("admin", "newpass123")
	if err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}
	if !user.HasRecovery {
		t.Fatalf("Expected HasRecovery to still be true")
	}

	// UpdateProfile
	updated, err := mgr.UpdateProfile(user.ID, "Super Admin")
	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}
	if updated.DisplayName != "Super Admin" {
		t.Fatalf("Expected display name 'Super Admin', got %q", updated.DisplayName)
	}
}

func TestBcryptCostWorkFactor(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_bcrypt_cost.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	mgr := auth.NewManager(database, true)

	// Setup initial admin
	_, _, err = mgr.Setup("admin", "secret123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	var hash string
	err = database.QueryRow("SELECT password_hash FROM users WHERE username = 'admin'").Scan(&hash)
	if err != nil {
		t.Fatalf("failed to query password hash: %v", err)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("failed to get bcrypt cost: %v", err)
	}
	if cost != 12 {
		t.Fatalf("expected bcrypt cost 12, got %d", cost)
	}
}

func TestRequireAuthMiddleware(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_req_auth.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	mgr := auth.NewManager(database, true)
	_, token, err := mgr.Setup("admin", "secret123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	protected := mgr.RequireAuthMiddleware(dummyHandler)

	// Case 1: Unauthenticated request to /api/v1/keys -> 401
	req1 := httptest.NewRequest("GET", "/api/v1/keys", nil)
	rec1 := httptest.NewRecorder()
	protected.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/v1/keys, got %d", rec1.Code)
	}

	// Case 2: Invalid session cookie to /api/v1/keys -> 401
	req2 := httptest.NewRequest("GET", "/api/v1/keys", nil)
	req2.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "invalid-token"})
	rec2 := httptest.NewRecorder()
	protected.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid session cookie on /api/v1/keys, got %d", rec2.Code)
	}

	// Case 3: Valid session cookie to /api/v1/keys -> 200
	req3 := httptest.NewRequest("GET", "/api/v1/keys", nil)
	req3.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	rec3 := httptest.NewRecorder()
	protected.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid session on /api/v1/keys, got %d", rec3.Code)
	}

	// Case 4: Public auth endpoint exempt /api/v1/auth/status -> 200
	req4 := httptest.NewRequest("GET", "/api/v1/auth/status", nil)
	rec4 := httptest.NewRecorder()
	protected.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("expected 200 for exempt public auth endpoint, got %d", rec4.Code)
	}

	// Case 5: Public config endpoint exempt /api/v1/config -> 200
	req5 := httptest.NewRequest("GET", "/api/v1/config", nil)
	rec5 := httptest.NewRecorder()
	protected.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("expected 200 for exempt config endpoint, got %d", rec5.Code)
	}

	// Case 6: Proxy endpoint exempt from session auth /v1/models -> 200 (handled by proxy key auth)
	req6 := httptest.NewRequest("GET", "/v1/models", nil)
	rec6 := httptest.NewRecorder()
	protected.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusOK {
		t.Fatalf("expected 200 for exempt proxy endpoint, got %d", rec6.Code)
	}
}
