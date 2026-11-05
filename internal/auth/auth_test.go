package auth_test

import (
	"errors"
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

func TestLoginUnknownUserDummyBcrypt(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_login_unknown.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	mgr := auth.NewManager(database, true)
	if _, _, err := mgr.Setup("admin", "admin123"); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	u, tok, err := mgr.Login("nonexistent_user", "password123")
	if err == nil {
		t.Fatalf("Expected error for nonexistent user, got user=%v, token=%q", u, tok)
	}
	if err.Error() != "invalid username or password" {
		t.Fatalf("Expected 'invalid username or password', got: %v", err)
	}
}

func TestDecoyRecoveryQuestion(t *testing.T) {
	q1 := auth.DecoyRecoveryQuestion("Alice")
	q2 := auth.DecoyRecoveryQuestion("Alice")
	q3 := auth.DecoyRecoveryQuestion("alice")
	q4 := auth.DecoyRecoveryQuestion("  alice  ")

	if q1 != q2 {
		t.Fatalf("Expected identical decoy questions for identical usernames: %q vs %q", q1, q2)
	}
	if q1 != q3 {
		t.Fatalf("Expected case-insensitive decoy questions: %q vs %q", q1, q3)
	}
	if q1 != q4 {
		t.Fatalf("Expected trimmed decoy questions: %q vs %q", q1, q4)
	}

	found := false
	for _, preset := range auth.PresetRecoveryQuestions {
		if q1 == preset {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Decoy question %q not in PresetRecoveryQuestions", q1)
	}
}

func TestRecoverPasswordDeletesSessions(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_recover_sessions.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	mgr := auth.NewManager(database, true)
	user, sessionToken, err := mgr.Setup("admin", "admin123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	if err := mgr.SetRecoveryQuestion(user.ID, "What is your childhood nickname?", "Ace"); err != nil {
		t.Fatalf("SetRecoveryQuestion failed: %v", err)
	}

	// Verify session token is valid
	sessUser, err := mgr.ValidateSession(sessionToken)
	if err != nil || sessUser == nil {
		t.Fatalf("Expected session to be valid before recovery: %v", err)
	}

	// Perform recovery
	if err := mgr.RecoverPassword("admin", "Ace", "newsecret123"); err != nil {
		t.Fatalf("RecoverPassword failed: %v", err)
	}

	// Verify session was deleted
	sessUserAfter, err := mgr.ValidateSession(sessionToken)
	if err == nil || sessUserAfter != nil {
		t.Fatalf("Expected session to be invalidated after recovery, but session was still valid")
	}

	var sessionCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", user.ID).Scan(&sessionCount); err != nil {
		t.Fatalf("Failed to count sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("Expected 0 sessions after password recovery, found %d", sessionCount)
	}
}

func TestRecoverPasswordErrorReasons(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_recover_reasons.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	mgr := auth.NewManager(database, true)
	user, _, err := mgr.Setup("admin", "admin123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// 1. Unknown user
	err = mgr.RecoverPassword("unknown_user", "someanswer", "newsecret123")
	if err == nil {
		t.Fatalf("Expected error for unknown user")
	}
	if err.Error() != "Invalid username or recovery answer" {
		t.Fatalf("Expected generic error message, got %q", err.Error())
	}
	var recErr *auth.RecoveryError
	if !errors.As(err, &recErr) || recErr.InternalReason != "user not found" {
		t.Fatalf("Expected internal reason 'user not found', got %v", recErr)
	}

	// 2. Existing user without recovery question
	err = mgr.RecoverPassword("admin", "someanswer", "newsecret123")
	if err == nil {
		t.Fatalf("Expected error for user without recovery question")
	}
	if err.Error() != "Invalid username or recovery answer" {
		t.Fatalf("Expected generic error message, got %q", err.Error())
	}
	if !errors.As(err, &recErr) || recErr.InternalReason != "no recovery question configured" {
		t.Fatalf("Expected internal reason 'no recovery question configured', got %v", recErr)
	}

	// Configure recovery question
	if err := mgr.SetRecoveryQuestion(user.ID, "What is your childhood nickname?", "Ace"); err != nil {
		t.Fatalf("SetRecoveryQuestion failed: %v", err)
	}

	// 3. Existing user with wrong answer
	err = mgr.RecoverPassword("admin", "wronganswer", "newsecret123")
	if err == nil {
		t.Fatalf("Expected error for wrong answer")
	}
	if err.Error() != "Invalid username or recovery answer" {
		t.Fatalf("Expected generic error message, got %q", err.Error())
	}
	if !errors.As(err, &recErr) || recErr.InternalReason != "incorrect recovery answer" {
		t.Fatalf("Expected internal reason 'incorrect recovery answer', got %v", recErr)
	}
}
