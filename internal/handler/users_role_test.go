package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/syslog"
)

func TestUserManagementRoleAccessAndReset(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_users_role.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	syslogMgr := syslog.NewManager(database)
	defer syslogMgr.Close()

	authMgr := auth.NewManager(database, true)

	// 1. Initial setup: create admin1
	admin1, _, err := authMgr.Setup("admin1", "Admin1Secret!")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Create admin2
	admin2, err := authMgr.CreateUser("admin2", "Admin Two", "Admin2Secret!", "admin")
	if err != nil {
		t.Fatalf("Create admin2 failed: %v", err)
	}

	// Create operator1
	operator1, err := authMgr.CreateUser("operator1", "Operator One", "Operator1Secret!", "operator")
	if err != nil {
		t.Fatalf("Create operator1 failed: %v", err)
	}

	// Set recovery question for operator1
	err = authMgr.SetRecoveryQuestion(operator1.ID, "What is your pet name?", "fluffy")
	if err != nil {
		t.Fatalf("Set recovery question failed: %v", err)
	}

	h := handler.New(authMgr, nil, nil, syslogMgr, nil, nil, nil, "http://localhost:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users", h.ListUsers)
	mux.HandleFunc("POST /api/v1/users", h.CreateUser)
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.DeleteUser)
	mux.HandleFunc("POST /api/v1/users/{id}/password", h.ResetUserPassword)
	mux.HandleFunc("POST /api/v1/users/{id}/reset-password", h.ResetUserPassword)
	mux.HandleFunc("POST /api/v1/users/{id}/reset-recovery", h.ResetUserRecovery)
	mux.HandleFunc("POST /api/v1/users/{id}/reset", h.ResetUser)

	router := authMgr.RequireAuthMiddleware(mux)

	// Login as admin1
	_, admin1Token, err := authMgr.Login("admin1", "Admin1Secret!")
	if err != nil {
		t.Fatalf("Login admin1 failed: %v", err)
	}
	admin1Cookie := &http.Cookie{Name: auth.SessionCookieName, Value: admin1Token}

	// Login as operator1
	_, operator1Token, err := authMgr.Login("operator1", "Operator1Secret!")
	if err != nil {
		t.Fatalf("Login operator1 failed: %v", err)
	}
	operator1Cookie := &http.Cookie{Name: auth.SessionCookieName, Value: operator1Token}

	// ── TEST 1: Operator attempts to manage users (MUST BE BLOCKED WITH 403) ──

	// Operator attempts GET /api/v1/users -> 403
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	req.AddCookie(operator1Cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Operator GET /api/v1/users expected 403, got %d", rec.Code)
	}

	// Operator attempts POST /api/v1/users (create user) -> 403
	createBody, _ := json.Marshal(map[string]string{
		"username": "rogue_user",
		"password": "RoguePassword123!",
		"role":     "admin",
	})
	req = httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(createBody))
	req.AddCookie(operator1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Operator POST /api/v1/users expected 403, got %d", rec.Code)
	}

	// Operator attempts DELETE /api/v1/users/1 -> 403
	req = httptest.NewRequest("DELETE", "/api/v1/users/1", nil)
	req.AddCookie(operator1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Operator DELETE /api/v1/users/1 expected 403, got %d", rec.Code)
	}

	// Operator attempts POST /api/v1/users/1/reset-password -> 403
	resetPassBody, _ := json.Marshal(map[string]string{"password": "NewSecret123!"})
	req = httptest.NewRequest("POST", "/api/v1/users/1/reset-password", bytes.NewReader(resetPassBody))
	req.AddCookie(operator1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Operator POST reset-password expected 403, got %d", rec.Code)
	}

	// ── TEST 2: Admin can list users and see recovery status ──
	req = httptest.NewRequest("GET", "/api/v1/users", nil)
	req.AddCookie(admin1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Admin GET /api/v1/users expected 200, got %d", rec.Code)
	}

	var usersList []auth.User
	if err := json.NewDecoder(rec.Body).Decode(&usersList); err != nil {
		t.Fatalf("Failed to decode users list: %v", err)
	}
	if len(usersList) != 3 {
		t.Fatalf("Expected 3 users, got %d", len(usersList))
	}

	// Verify operator1 has_recovery = true
	var foundOp *auth.User
	for i := range usersList {
		if usersList[i].Username == "operator1" {
			foundOp = &usersList[i]
			break
		}
	}
	if foundOp == nil {
		t.Fatalf("operator1 not found in users list")
	}
	if !foundOp.HasRecovery {
		t.Errorf("expected operator1 to have HasRecovery = true")
	}

	// ── TEST 3: Admin cannot reset another Admin ──

	// Reset password of admin2 by admin1 -> MUST FAIL WITH 403
	req = httptest.NewRequest("POST", "/api/v1/users/"+strconv.FormatInt(admin2.ID, 10)+"/reset-password", bytes.NewReader(resetPassBody))
	req.AddCookie(admin1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Admin reset of admin2 password expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// Reset recovery of admin2 by admin1 -> MUST FAIL WITH 403
	req = httptest.NewRequest("POST", "/api/v1/users/"+strconv.FormatInt(admin2.ID, 10)+"/reset-recovery", nil)
	req.AddCookie(admin1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Admin reset of admin2 recovery expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// ── TEST 4: Admin CAN reset Operator (non-admin) ──

	// 4a. Reset password of operator1
	newOpPass := "BrandNewOpPassword123!"
	resetPassBody, _ = json.Marshal(map[string]string{"password": newOpPass})
	req = httptest.NewRequest("POST", "/api/v1/users/"+strconv.FormatInt(operator1.ID, 10)+"/reset-password", bytes.NewReader(resetPassBody))
	req.AddCookie(admin1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Admin reset operator1 password expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify operator1 can log in with new password
	_, _, err = authMgr.Login("operator1", newOpPass)
	if err != nil {
		t.Fatalf("operator1 login with new password failed: %v", err)
	}

	// Verify old password fails
	_, _, err = authMgr.Login("operator1", "Operator1Secret!")
	if err == nil {
		t.Fatalf("operator1 login with old password should fail, but succeeded")
	}

	// 4b. Reset recovery question of operator1
	req = httptest.NewRequest("POST", "/api/v1/users/"+strconv.FormatInt(operator1.ID, 10)+"/reset-recovery", nil)
	req.AddCookie(admin1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Admin reset operator1 recovery expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify operator1 now has has_recovery = false
	req = httptest.NewRequest("GET", "/api/v1/users", nil)
	req.AddCookie(admin1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var updatedList []auth.User
	_ = json.NewDecoder(rec.Body).Decode(&updatedList)
	for _, u := range updatedList {
		if u.Username == "operator1" {
			if u.HasRecovery {
				t.Errorf("expected operator1 to have HasRecovery = false after reset, got true")
			}
			break
		}
	}

	// ── TEST 5: Admin cannot delete own account ──
	req = httptest.NewRequest("DELETE", "/api/v1/users/"+strconv.FormatInt(admin1.ID, 10), nil)
	req.AddCookie(admin1Cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Admin deleting self expected 400, got %d", rec.Code)
	}
}
