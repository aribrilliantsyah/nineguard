package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"nineguard/internal/db"
)

type contextKey string

const userContextKey contextKey = "user"
const SessionCookieName = "nineguard_session"
const bcryptCost = 12

type User struct {
	ID               int64      `json:"id"`
	Username         string     `json:"username"`
	DisplayName      string     `json:"display_name"`
	Role             string     `json:"role"` // admin | operator
	CreatedAt        time.Time  `json:"created_at"`
	LastLoginAt      *time.Time `json:"last_login_at,omitempty"`
	HasRecovery      bool       `json:"has_recovery"`
	RecoveryQuestion string     `json:"recovery_question,omitempty"`
}

type Manager struct {
	db          *db.DB
	authEnabled bool
}

func NewManager(database *db.DB, authEnabled bool) *Manager {
	return &Manager{
		db:          database,
		authEnabled: authEnabled,
	}
}

func (m *Manager) IsAuthEnabled() bool {
	return m.authEnabled
}

func (m *Manager) NeedsSetup() (bool, error) {
	var count int
	err := m.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (m *Manager) Setup(username, password string) (*User, string, error) {
	needs, err := m.NeedsSetup()
	if err != nil {
		return nil, "", err
	}
	if !needs {
		return nil, "", errors.New("setup has already been completed")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, "", err
	}

	res, err := m.db.Exec(`
		INSERT INTO users (username, display_name, password_hash, role, last_login_at)
		VALUES (?, ?, ?, 'admin', CURRENT_TIMESTAMP)
	`, username, username, string(hash))
	if err != nil {
		return nil, "", err
	}

	uid, err := res.LastInsertId()
	if err != nil {
		return nil, "", err
	}

	token, err := m.createSession(uid)
	if err != nil {
		return nil, "", err
	}

	slog.Info("setup completed", "source", "audit", "actor_id", uid, "username", username, "action", "auth.setup")

	user := &User{
		ID:          uid,
		Username:    username,
		DisplayName: username,
		Role:        "admin",
		CreatedAt:   time.Now(),
	}

	return user, token, nil
}

func (m *Manager) Login(username, password string) (*User, string, error) {
	var user User
	var hash string
	var lastLogin sql.NullTime
	var recQ, recAns sql.NullString

	err := m.db.QueryRow(`
		SELECT id, username, display_name, password_hash, role, created_at, last_login_at, recovery_question, recovery_answer_hash
		FROM users WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.DisplayName, &hash, &user.Role, &user.CreatedAt, &lastLogin, &recQ, &recAns)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Warn("login failure: user not found", "source", "audit", "username", username, "action", "auth.login.failure")
			return nil, "", errors.New("invalid username or password")
		}
		return nil, "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		slog.Warn("login failure: invalid password", "source", "audit", "actor_id", user.ID, "username", username, "action", "auth.login.failure")
		return nil, "", errors.New("invalid username or password")
	}

	if lastLogin.Valid {
		user.LastLoginAt = &lastLogin.Time
	}

	user.HasRecovery = recAns.Valid && strings.TrimSpace(recAns.String) != ""
	if recQ.Valid {
		user.RecoveryQuestion = recQ.String
	}

	_, _ = m.db.Exec("UPDATE users SET last_login_at = CURRENT_TIMESTAMP WHERE id = ?", user.ID)

	token, err := m.createSession(user.ID)
	if err != nil {
		return nil, "", err
	}

	slog.Info("login success", "source", "audit", "actor_id", user.ID, "username", user.Username, "action", "auth.login.success")

	return &user, token, nil
}

func (m *Manager) Logout(token string) error {
	slog.Info("logout", "source", "audit", "action", "auth.logout")
	_, err := m.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

func (m *Manager) createSession(userID int64) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)
	expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30 days session

	_, err := m.db.Exec("INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)", token, userID, expiresAt)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (m *Manager) ValidateSession(token string) (*User, error) {
	var user User
	var expiresAt time.Time
	var lastLogin sql.NullTime
	var recQ, recAns sql.NullString

	err := m.db.QueryRow(`
		SELECT u.id, u.username, u.display_name, u.role, u.created_at, u.last_login_at, s.expires_at, u.recovery_question, u.recovery_answer_hash
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token = ?
	`, token).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.CreatedAt, &lastLogin, &expiresAt, &recQ, &recAns)

	if err != nil {
		return nil, err
	}

	if time.Now().After(expiresAt) {
		_, _ = m.db.Exec("DELETE FROM sessions WHERE token = ?", token)
		return nil, errors.New("session expired")
	}

	if lastLogin.Valid {
		user.LastLoginAt = &lastLogin.Time
	}

	user.HasRecovery = recAns.Valid && strings.TrimSpace(recAns.String) != ""
	if recQ.Valid {
		user.RecoveryQuestion = recQ.String
	}

	return &user, nil
}

func (m *Manager) ListUsers() ([]User, error) {
	rows, err := m.db.Query("SELECT id, username, display_name, role, created_at, last_login_at, recovery_question, recovery_answer_hash FROM users ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var u User
		var lastLogin sql.NullTime
		var recQ, recAns sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &lastLogin, &recQ, &recAns); err != nil {
			return nil, err
		}
		if lastLogin.Valid {
			u.LastLoginAt = &lastLogin.Time
		}
		u.HasRecovery = recAns.Valid && strings.TrimSpace(recAns.String) != ""
		if recQ.Valid {
			u.RecoveryQuestion = recQ.String
		}
		list = append(list, u)
	}
	return list, nil
}

func (m *Manager) CreateUser(username, displayName, password, role string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, err
	}
	if role != "admin" {
		role = "operator"
	}
	res, err := m.db.Exec("INSERT INTO users (username, display_name, password_hash, role) VALUES (?, ?, ?, ?)", username, displayName, string(hash), role)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	slog.Info("user created", "source", "audit", "actor_id", id, "username", username, "role", role, "action", "user.create")
	return &User{
		ID:          id,
		Username:    username,
		DisplayName: displayName,
		Role:        role,
		CreatedAt:   time.Now(),
	}, nil
}

func (m *Manager) UpdateUser(id int64, username, displayName, role string) (*User, error) {
	if role != "admin" {
		role = "operator"
	}
	_, err := m.db.Exec("UPDATE users SET username = ?, display_name = ?, role = ? WHERE id = ?", username, displayName, role, id)
	if err != nil {
		return nil, err
	}
	var u User
	var lastLogin sql.NullTime
	var recQ, recAns sql.NullString
	err = m.db.QueryRow("SELECT id, username, display_name, role, created_at, last_login_at, recovery_question, recovery_answer_hash FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &lastLogin, &recQ, &recAns)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	u.HasRecovery = recAns.Valid && strings.TrimSpace(recAns.String) != ""
	if recQ.Valid {
		u.RecoveryQuestion = recQ.String
	}
	return &u, nil
}

func (m *Manager) ChangePassword(userID int64, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(hash), userID)
	if err == nil {
		slog.Info("password changed", "source", "audit", "actor_id", userID, "action", "auth.password.change")
	}
	return err
}

func (m *Manager) DeleteUser(id int64) error {
	var count int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	var role string
	_ = m.db.QueryRow("SELECT role FROM users WHERE id = ?", id).Scan(&role)
	if role == "admin" && count <= 1 {
		return errors.New("cannot delete the last administrator")
	}
	_, err := m.db.Exec("DELETE FROM users WHERE id = ?", id)
	if err == nil {
		slog.Info("user deleted", "source", "audit", "target_user_id", id, "action", "user.delete")
	}
	return err
}

func (m *Manager) ResetUserPassword(targetUserID int64, newPassword string) error {
	if len(newPassword) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	var role string
	err := m.db.QueryRow("SELECT role FROM users WHERE id = ?", targetUserID).Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("user not found")
		}
		return err
	}
	if role == "admin" {
		return errors.New("cannot reset password of an administrator account")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(hash), targetUserID)
	if err == nil {
		_, _ = m.db.Exec("DELETE FROM sessions WHERE user_id = ?", targetUserID)
		slog.Info("user password reset by admin", "source", "audit", "target_user_id", targetUserID, "action", "user.reset_password")
	}
	return err
}

func (m *Manager) ResetUserRecovery(targetUserID int64) error {
	var role string
	err := m.db.QueryRow("SELECT role FROM users WHERE id = ?", targetUserID).Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("user not found")
		}
		return err
	}
	if role == "admin" {
		return errors.New("cannot reset recovery question of an administrator account")
	}

	_, err = m.db.Exec("UPDATE users SET recovery_question = '', recovery_answer_hash = '' WHERE id = ?", targetUserID)
	if err == nil {
		slog.Info("user recovery question reset by admin", "source", "audit", "target_user_id", targetUserID, "action", "user.reset_recovery")
	}
	return err
}

func (m *Manager) SetRecoveryQuestion(userID int64, question, answer string) error {
	trimmedQ := strings.TrimSpace(question)
	trimmedAns := strings.TrimSpace(strings.ToLower(answer))
	if trimmedQ == "" {
		return errors.New("recovery question cannot be empty")
	}
	if len(trimmedAns) < 2 {
		return errors.New("recovery answer must be at least 2 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(trimmedAns), bcryptCost)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE users SET recovery_question = ?, recovery_answer_hash = ? WHERE id = ?", trimmedQ, string(hash), userID)
	return err
}

func (m *Manager) GetRecoveryQuestion(username string) (string, error) {
	var q, ans sql.NullString
	err := m.db.QueryRow("SELECT recovery_question, recovery_answer_hash FROM users WHERE username = ?", strings.TrimSpace(username)).Scan(&q, &ans)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("user not found")
		}
		return "", err
	}
	if !ans.Valid || strings.TrimSpace(ans.String) == "" || !q.Valid || strings.TrimSpace(q.String) == "" {
		return "", errors.New("no recovery question configured for this account")
	}
	return q.String, nil
}

func (m *Manager) RecoverPassword(username, answer, newPassword string) error {
	if len(newPassword) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	var id int64
	var ansHash string
	err := m.db.QueryRow("SELECT id, recovery_answer_hash FROM users WHERE username = ?", strings.TrimSpace(username)).Scan(&id, &ansHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("invalid username or recovery answer")
		}
		return err
	}
	if strings.TrimSpace(ansHash) == "" {
		return errors.New("no recovery question configured for this account")
	}

	trimmedAns := strings.TrimSpace(strings.ToLower(answer))
	if err := bcrypt.CompareHashAndPassword([]byte(ansHash), []byte(trimmedAns)); err != nil {
		slog.Warn("password recovery failure: incorrect answer", "source", "audit", "username", username, "action", "auth.password.recover.failure")
		return errors.New("incorrect recovery answer")
	}

	err = m.ChangePassword(id, newPassword)
	if err == nil {
		slog.Info("password recovered successfully", "source", "audit", "actor_id", id, "username", username, "action", "auth.password.recover.success")
	}
	return err
}

func (m *Manager) UpdateProfile(userID int64, displayName string) (*User, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil, errors.New("display name cannot be empty")
	}
	_, err := m.db.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, userID)
	if err != nil {
		return nil, err
	}
	var u User
	var lastLogin sql.NullTime
	var recQ, recAns sql.NullString
	err = m.db.QueryRow("SELECT id, username, display_name, role, created_at, last_login_at, recovery_question, recovery_answer_hash FROM users WHERE id = ?", userID).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &lastLogin, &recQ, &recAns)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	u.HasRecovery = recAns.Valid && strings.TrimSpace(recAns.String) != ""
	if recQ.Valid {
		u.RecoveryQuestion = recQ.String
	}
	return &u, nil
}

func ContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

func UserFromContext(ctx context.Context) *User {
	if u, ok := ctx.Value(userContextKey).(*User); ok {
		return u
	}
	return nil
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.authEnabled {
			// Auth disabled: inject dummy admin
			dummy := &User{ID: 1, Username: "admin", DisplayName: "Administrator", Role: "admin", HasRecovery: true}
			ctx := context.WithValue(r.Context(), userContextKey, dummy)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		user, err := m.ValidateSession(cookie.Value)
		if err == nil && user != nil {
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (m *Manager) RequireAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Exempt proxy (/v1/*), static UI assets, health check, and public auth endpoints
		if !strings.HasPrefix(path, "/api/v1/") ||
			strings.HasPrefix(path, "/api/v1/auth/") ||
			path == "/api/v1/config" {
			m.Middleware(next).ServeHTTP(w, r)
			return
		}

		if !m.authEnabled {
			m.Middleware(next).ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			http.Error(w, `{"error":"Authentication required"}`, http.StatusUnauthorized)
			return
		}

		user, err := m.ValidateSession(cookie.Value)
		if err != nil || user == nil {
			http.Error(w, `{"error":"Invalid or expired session"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
