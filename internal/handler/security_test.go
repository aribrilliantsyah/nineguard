package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/syslog"
)

func TestSecureCookieAttribute(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_secure_cookie.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	authMgr := auth.NewManager(database, true)
	_, _, err = authMgr.Setup("admin", "admin123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	h := handler.New(authMgr, nil, nil, nil, nil, nil, nil, "http://localhost:8080")

	// 1. Request over plain HTTP without forwarding header -> Secure should be false
	loginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	reqHTTP := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	recHTTP := httptest.NewRecorder()
	h.AuthLogin(recHTTP, reqHTTP)

	cookiesHTTP := recHTTP.Result().Cookies()
	var sessionCookieHTTP *http.Cookie
	for _, c := range cookiesHTTP {
		if c.Name == auth.SessionCookieName {
			sessionCookieHTTP = c
			break
		}
	}
	if sessionCookieHTTP == nil {
		t.Fatalf("session cookie not found in HTTP response")
	}
	if sessionCookieHTTP.Secure {
		t.Errorf("expected cookie Secure=false for plain HTTP, got true")
	}

	// 2. Request with X-Forwarded-Proto: https -> Secure should be true
	reqHTTPS := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	reqHTTPS.Header.Set("X-Forwarded-Proto", "https")
	recHTTPS := httptest.NewRecorder()
	h.AuthLogin(recHTTPS, reqHTTPS)

	cookiesHTTPS := recHTTPS.Result().Cookies()
	var sessionCookieHTTPS *http.Cookie
	for _, c := range cookiesHTTPS {
		if c.Name == auth.SessionCookieName {
			sessionCookieHTTPS = c
			break
		}
	}
	if sessionCookieHTTPS == nil {
		t.Fatalf("session cookie not found in HTTPS response")
	}
	if !sessionCookieHTTPS.Secure {
		t.Errorf("expected cookie Secure=true for X-Forwarded-Proto: https, got false")
	}
}

func TestAuditLogRecording(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_audit.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	syslogMgr := syslog.NewManager(database)

	authMgr := auth.NewManager(database, true)
	h := handler.New(authMgr, nil, nil, syslogMgr, nil, nil, nil, "http://localhost:8080")

	// Perform setup -> should record audit log
	setupBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret123"})
	reqSetup := httptest.NewRequest("POST", "/api/v1/auth/setup", bytes.NewReader(setupBody))
	recSetup := httptest.NewRecorder()
	h.AuthSetup(recSetup, reqSetup)

	if recSetup.Code != http.StatusOK {
		t.Fatalf("AuthSetup failed: %d", recSetup.Code)
	}

	// Login failure -> should record audit log
	failBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrongpassword"})
	reqFail := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(failBody))
	recFail := httptest.NewRecorder()
	h.AuthLogin(recFail, reqFail)

	if recFail.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on login failure, got %d", recFail.Code)
	}

	// Flush async syslog worker
	syslogMgr.Close()

	// Query system_logs table directly to verify structured audit logs exist
	rows, err := database.Query("SELECT source, level, message, attrs FROM system_logs WHERE source = 'audit'")
	if err != nil {
		t.Fatalf("failed to query system_logs: %v", err)
	}
	defer rows.Close()

	var auditEntriesCount int
	for rows.Next() {
		var src, lvl, msg, attrs string
		if err := rows.Scan(&src, &lvl, &msg, &attrs); err != nil {
			t.Fatalf("failed to scan log entry: %v", err)
		}
		auditEntriesCount++
		if !strings.Contains(attrs, "action") || !strings.Contains(attrs, "client_ip") {
			t.Errorf("expected structured attrs with action and client_ip, got %s", attrs)
		}
	}
	if auditEntriesCount < 2 {
		t.Errorf("expected at least 2 audit entries recorded (setup and login failure), got %d", auditEntriesCount)
	}
}
