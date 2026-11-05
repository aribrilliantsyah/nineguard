package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/handler"
)

func setupTestHandler(t *testing.T) (*handler.Handler, *auth.Manager, *db.DB) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_enum.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	authMgr := auth.NewManager(database, true)
	h := handler.New(authMgr, nil, nil, nil, nil, nil, nil, "http://localhost:8080")
	return h, authMgr, database
}

func TestRecoveryGET_AntiEnumeration(t *testing.T) {
	h, authMgr, database := setupTestHandler(t)
	defer database.Close()

	// 1. Setup admin without recovery question
	admin, _, err := authMgr.Setup("admin", "admin123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// 2. Create operator with recovery question
	op, err := authMgr.CreateUser("operator", "Operator One", "operator123", "operator")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	realQuestion := "What was the name of your first pet?"
	if err := authMgr.SetRecoveryQuestion(op.ID, realQuestion, "Fluffy"); err != nil {
		t.Fatalf("SetRecoveryQuestion failed: %v", err)
	}

	// Case A: Unknown user gets 200 and a decoy question
	reqUnknown := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=nonexistent_user", nil)
	reqUnknown.RemoteAddr = "10.1.0.1:1234"
	wUnknown := httptest.NewRecorder()
	h.GetRecoveryQuestion(wUnknown, reqUnknown)

	if wUnknown.Code != http.StatusOK {
		t.Fatalf("Expected 200 for unknown user, got %d: %s", wUnknown.Code, wUnknown.Body.String())
	}
	var respUnknown map[string]interface{}
	if err := json.NewDecoder(wUnknown.Body).Decode(&respUnknown); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if respUnknown["status"] != "ok" || respUnknown["username"] != "nonexistent_user" || respUnknown["question"] == "" {
		t.Fatalf("Unexpected shape for unknown user: %+v", respUnknown)
	}

	// Case B: Same unknown user queried twice gets identical decoy question
	reqUnknown2 := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=nonexistent_user", nil)
	reqUnknown2.RemoteAddr = "10.1.0.2:1234"
	wUnknown2 := httptest.NewRecorder()
	h.GetRecoveryQuestion(wUnknown2, reqUnknown2)

	var respUnknown2 map[string]interface{}
	_ = json.NewDecoder(wUnknown2.Body).Decode(&respUnknown2)
	if respUnknown2["question"] != respUnknown["question"] {
		t.Fatalf("Expected identical decoy for same username, got %q vs %q", respUnknown["question"], respUnknown2["question"])
	}

	// Case C: Existing user without configured question gets 200 and decoy question
	reqNoQ := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=admin", nil)
	reqNoQ.RemoteAddr = "10.1.0.3:1234"
	wNoQ := httptest.NewRecorder()
	h.GetRecoveryQuestion(wNoQ, reqNoQ)

	if wNoQ.Code != http.StatusOK {
		t.Fatalf("Expected 200 for user without question, got %d: %s", wNoQ.Code, wNoQ.Body.String())
	}
	var respNoQ map[string]interface{}
	if err := json.NewDecoder(wNoQ.Body).Decode(&respNoQ); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if respNoQ["status"] != "ok" || respNoQ["username"] != "admin" || respNoQ["question"] == "" {
		t.Fatalf("Unexpected shape for user without question: %+v", respNoQ)
	}

	// Case D: Existing user with configured question gets 200 and REAL question
	reqReal := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=operator", nil)
	reqReal.RemoteAddr = "10.1.0.4:1234"
	wReal := httptest.NewRecorder()
	h.GetRecoveryQuestion(wReal, reqReal)

	if wReal.Code != http.StatusOK {
		t.Fatalf("Expected 200 for user with question, got %d: %s", wReal.Code, wReal.Body.String())
	}
	var respReal map[string]interface{}
	if err := json.NewDecoder(wReal.Body).Decode(&respReal); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if respReal["question"] != realQuestion {
		t.Fatalf("Expected real question %q, got %q", realQuestion, respReal["question"])
	}

	// Verify all response shapes are strictly identical
	for key := range respUnknown {
		if _, ok := respNoQ[key]; !ok {
			t.Fatalf("Key %q missing in respNoQ", key)
		}
		if _, ok := respReal[key]; !ok {
			t.Fatalf("Key %q missing in respReal", key)
		}
	}
	_ = admin

	// Empty username returns 400
	reqEmpty := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=", nil)
	reqEmpty.RemoteAddr = "10.1.0.5:1234"
	wEmpty := httptest.NewRecorder()
	h.GetRecoveryQuestion(wEmpty, reqEmpty)
	if wEmpty.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for empty username, got %d", wEmpty.Code)
	}
}

func TestRecoveryPOST_AntiEnumeration(t *testing.T) {
	h, authMgr, database := setupTestHandler(t)
	defer database.Close()

	// Admin with question
	admin, _, err := authMgr.Setup("admin", "admin123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := authMgr.SetRecoveryQuestion(admin.ID, "What is your childhood nickname?", "Ace"); err != nil {
		t.Fatalf("SetRecoveryQuestion failed: %v", err)
	}

	// User without question
	_, err = authMgr.CreateUser("no_question_user", "No Q", "pass123", "operator")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	makeReq := func(username, answer, ip string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{
			"username":     username,
			"answer":       answer,
			"new_password": "newpassword123",
		})
		req := httptest.NewRequest("POST", "/api/v1/auth/recovery", bytes.NewReader(body))
		req.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		h.RecoverPassword(w, req)
		return w
	}

	// 1. Unknown user
	wUnknown := makeReq("ghost_user", "Ace", "10.2.0.1")
	// 2. User without question
	wNoQ := makeReq("no_question_user", "Ace", "10.2.0.2")
	// 3. User with wrong answer
	wWrong := makeReq("admin", "WrongAnswer", "10.2.0.3")

	if wUnknown.Code != http.StatusBadRequest {
		t.Fatalf("Unknown user status %d, expected 400", wUnknown.Code)
	}
	if wNoQ.Code != http.StatusBadRequest {
		t.Fatalf("No question user status %d, expected 400", wNoQ.Code)
	}
	if wWrong.Code != http.StatusBadRequest {
		t.Fatalf("Wrong answer user status %d, expected 400", wWrong.Code)
	}

	bodyUnknown := wUnknown.Body.String()
	bodyNoQ := wNoQ.Body.String()
	bodyWrong := wWrong.Body.String()

	if bodyUnknown != bodyNoQ || bodyUnknown != bodyWrong {
		t.Fatalf("Response bodies differ across failure cases:\nUnknown: %s\nNoQ:     %s\nWrong:   %s", bodyUnknown, bodyNoQ, bodyWrong)
	}

	var jsonBody map[string]string
	if err := json.Unmarshal([]byte(bodyUnknown), &jsonBody); err != nil {
		t.Fatalf("Failed to parse response body: %v", err)
	}
	if jsonBody["error"] != "Invalid username or recovery answer" {
		t.Fatalf("Expected 'Invalid username or recovery answer', got %q", jsonBody["error"])
	}
}

func TestRecoveryPOST_LockoutAndClockAdvance(t *testing.T) {
	h, authMgr, database := setupTestHandler(t)
	defer database.Close()

	admin, _, err := authMgr.Setup("admin", "originalpass123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := authMgr.SetRecoveryQuestion(admin.ID, "What is your childhood nickname?", "Ace"); err != nil {
		t.Fatalf("SetRecoveryQuestion failed: %v", err)
	}

	limiter := handler.NewRateLimiter()
	defer limiter.Stop()
	currentTime := time.Now()
	limiter.SetNow(func() time.Time { return currentTime })
	h.SetRateLimiter(limiter)

	// 5 consecutive wrong answers (from different IPs to avoid triggering per-IP limit)
	for i := 1; i <= 5; i++ {
		body, _ := json.Marshal(map[string]string{
			"username":     "admin",
			"answer":       "WrongAnswer",
			"new_password": "newpassword123",
		})
		req := httptest.NewRequest("POST", "/api/v1/auth/recovery", bytes.NewReader(body))
		req.RemoteAddr = "10.3.0." + strconv.Itoa(i) + ":1234"
		w := httptest.NewRecorder()
		h.RecoverPassword(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("Attempt %d expected 400, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	// 6th attempt with CORRECT answer while locked -> must be rejected with 429
	bodyCorrect, _ := json.Marshal(map[string]string{
		"username":     "admin",
		"answer":       "Ace",
		"new_password": "newpassword123",
	})
	req6 := httptest.NewRequest("POST", "/api/v1/auth/recovery", bytes.NewReader(bodyCorrect))
	req6.RemoteAddr = "10.3.0.6:1234"
	w6 := httptest.NewRecorder()
	h.RecoverPassword(w6, req6)

	if w6.Code != http.StatusTooManyRequests {
		t.Fatalf("Attempt 6 with correct answer expected 429, got %d: %s", w6.Code, w6.Body.String())
	}
	if w6.Header().Get("Retry-After") == "" {
		t.Fatalf("Expected Retry-After header on 429 response")
	}

	// Verify password in DB was NOT changed
	_, _, err = authMgr.Login("admin", "newpassword123")
	if err == nil {
		t.Fatalf("Password should NOT have changed during lockout")
	}
	_, _, err = authMgr.Login("admin", "originalpass123")
	if err != nil {
		t.Fatalf("Original password should still work: %v", err)
	}

	// Advance clock past the 15-minute lock window
	currentTime = currentTime.Add(15*time.Minute + 1*time.Second)

	// 7th attempt with correct answer after lockout expired -> should succeed
	req7 := httptest.NewRequest("POST", "/api/v1/auth/recovery", bytes.NewReader(bodyCorrect))
	req7.RemoteAddr = "10.3.0.7:1234"
	w7 := httptest.NewRecorder()
	h.RecoverPassword(w7, req7)

	if w7.Code != http.StatusOK {
		t.Fatalf("Attempt 7 after lock expired expected 200, got %d: %s", w7.Code, w7.Body.String())
	}

	// Verify password in DB is now changed
	_, _, err = authMgr.Login("admin", "newpassword123")
	if err != nil {
		t.Fatalf("New password should work after recovery: %v", err)
	}
}

func TestLogin_LockoutAndClockAdvance(t *testing.T) {
	h, authMgr, database := setupTestHandler(t)
	defer database.Close()

	_, _, err := authMgr.Setup("admin", "correctpassword123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	limiter := handler.NewRateLimiter()
	defer limiter.Stop()
	currentTime := time.Now()
	limiter.SetNow(func() time.Time { return currentTime })
	h.SetRateLimiter(limiter)

	// 5 failed login attempts
	for i := 1; i <= 5; i++ {
		body, _ := json.Marshal(map[string]string{
			"username": "admin",
			"password": "wrongpassword",
		})
		req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "10.4.0." + strconv.Itoa(i) + ":1234"
		w := httptest.NewRecorder()
		h.AuthLogin(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Login attempt %d expected 401, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	// 6th attempt with correct credentials while locked -> 429
	bodyCorrect, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "correctpassword123",
	})
	req6 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyCorrect))
	req6.RemoteAddr = "10.4.0.6:1234"
	w6 := httptest.NewRecorder()
	h.AuthLogin(w6, req6)

	if w6.Code != http.StatusTooManyRequests {
		t.Fatalf("Login attempt 6 expected 429, got %d: %s", w6.Code, w6.Body.String())
	}
	if w6.Header().Get("Retry-After") == "" {
		t.Fatalf("Expected Retry-After header on 429")
	}

	// Advance clock past 15-minute lockout
	currentTime = currentTime.Add(15*time.Minute + 1*time.Second)

	// 7th attempt with correct password after lock expiration -> 200
	req7 := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(bodyCorrect))
	req7.RemoteAddr = "10.4.0.7:1234"
	w7 := httptest.NewRecorder()
	h.AuthLogin(w7, req7)

	if w7.Code != http.StatusOK {
		t.Fatalf("Login attempt 7 after lock expiration expected 200, got %d: %s", w7.Code, w7.Body.String())
	}
}

func TestRateLimit_PerIP_Threshold(t *testing.T) {
	h, authMgr, database := setupTestHandler(t)
	defer database.Close()

	if _, _, err := authMgr.Setup("admin", "admin123"); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	const testIP = "198.51.100.42"

	// 10 requests from the same IP across the endpoints should succeed (not 429)
	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=testuser", nil)
		req.Header.Set("X-Forwarded-For", testIP)
		w := httptest.NewRecorder()
		h.GetRecoveryQuestion(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("Request %d unexpectedly rate-limited with 429: %s", i, w.Body.String())
		}
	}

	// 11th request from the same IP must be rate-limited (429)
	req11 := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=testuser", nil)
	req11.Header.Set("X-Forwarded-For", testIP)
	w11 := httptest.NewRecorder()
	h.GetRecoveryQuestion(w11, req11)

	if w11.Code != http.StatusTooManyRequests {
		t.Fatalf("Request 11 expected 429, got %d: %s", w11.Code, w11.Body.String())
	}
	retryAfter := w11.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatalf("Expected Retry-After header on 429 response")
	}
	var errResp map[string]string
	if err := json.NewDecoder(w11.Body).Decode(&errResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if errResp["error"] != "Too many attempts, try again later" {
		t.Fatalf("Expected 'Too many attempts, try again later', got %q", errResp["error"])
	}

	// Request from a different IP is NOT rate-limited
	reqOther := httptest.NewRequest("GET", "/api/v1/auth/recovery?username=testuser", nil)
	reqOther.Header.Set("X-Forwarded-For", "203.0.113.99")
	wOther := httptest.NewRecorder()
	h.GetRecoveryQuestion(wOther, reqOther)
	if wOther.Code != http.StatusOK {
		t.Fatalf("Request from other IP expected 200, got %d: %s", wOther.Code, wOther.Body.String())
	}
}

func TestLoginUnknownUserGenericError(t *testing.T) {
	h, authMgr, database := setupTestHandler(t)
	defer database.Close()

	if _, _, err := authMgr.Setup("admin", "admin123"); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	body, _ := json.Marshal(map[string]string{
		"username": "nobody_exists",
		"password": "wrongpassword",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "10.5.0.1:1234"
	w := httptest.NewRecorder()
	h.AuthLogin(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid username or password" {
		t.Fatalf("Expected 'invalid username or password', got %q", resp["error"])
	}
}

func TestRecoveryPOST_InvalidatesSessions(t *testing.T) {
	h, authMgr, database := setupTestHandler(t)
	defer database.Close()

	admin, sessionToken, err := authMgr.Setup("admin", "admin123")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := authMgr.SetRecoveryQuestion(admin.ID, "What is your childhood nickname?", "Ace"); err != nil {
		t.Fatalf("SetRecoveryQuestion failed: %v", err)
	}

	// Verify session works
	user, err := authMgr.ValidateSession(sessionToken)
	if err != nil || user == nil {
		t.Fatalf("Expected valid session before recovery: %v", err)
	}

	// Perform recovery POST
	body, _ := json.Marshal(map[string]string{
		"username":     "admin",
		"answer":       "Ace",
		"new_password": "brandnewpassword123",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/recovery", bytes.NewReader(body))
	req.RemoteAddr = "10.6.0.1:1234"
	w := httptest.NewRecorder()
	h.RecoverPassword(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Recovery POST expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify session token is invalidated
	userAfter, err := authMgr.ValidateSession(sessionToken)
	if err == nil || userAfter != nil {
		t.Fatalf("Expected session token to be invalid after password recovery")
	}
}
