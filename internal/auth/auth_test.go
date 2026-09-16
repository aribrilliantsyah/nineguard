package auth_test

import (
	"path/filepath"
	"testing"

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
