package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/auth"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/plugins"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/syslog"
	"nineguard/internal/timeutil"
	"nineguard/internal/traffic"
	"nineguard/internal/version"
)

type Handler struct {
	auth         *auth.Manager
	models       *models.Manager
	traffic      *traffic.Manager
	syslog       *syslog.Manager
	keys         *keys.Manager
	providers    *providers.Manager
	plugins      *plugins.Manager
	proxy        *proxy.Proxy
	routerTarget string
}

func (h *Handler) SetPlugins(plm *plugins.Manager) {
	h.plugins = plm
}

func New(am *auth.Manager, mm *models.Manager, tm *traffic.Manager, sm *syslog.Manager, km *keys.Manager, pm *providers.Manager, pr *proxy.Proxy, target string) *Handler {
	return &Handler{
		auth:         am,
		models:       mm,
		traffic:      tm,
		syslog:       sm,
		keys:         km,
		providers:    pm,
		proxy:        pr,
		routerTarget: target,
	}
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

func getClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return r.RemoteAddr
}

func (h *Handler) logAudit(r *http.Request, actorID int64, username, action, target, status, details string) {
	clientIP := getClientIP(r)
	if h.syslog != nil {
		h.syslog.RecordAudit(syslog.AuditEntry{
			Timestamp:      time.Now().UTC(),
			ActorID:        actorID,
			ActorUsername:  username,
			Action:         action,
			TargetResource: target,
			ClientIP:       clientIP,
			Status:         status,
			Details:        details,
		})
	}
	slog.Info("security audit",
		"source", "audit",
		"actor_id", actorID,
		"actor_username", username,
		"action", action,
		"target_resource", target,
		"client_ip", clientIP,
		"status", status,
	)
}

// ── Auth Handlers ──

func (h *Handler) AuthStatus(w http.ResponseWriter, r *http.Request) {
	needsSetup, _ := h.auth.NeedsSetup()
	user := auth.UserFromContext(r.Context())

	resp := map[string]interface{}{
		"auth_enabled":  h.auth.IsAuthEnabled(),
		"authenticated": user != nil,
		"setup_needed":  needsSetup,
	}
	if user != nil {
		resp["user"] = user
		resp["username"] = user.Username
	}
	jsonResponse(w, http.StatusOK, resp)
}

func (h *Handler) AuthSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(body.Username) < 2 || len(body.Password) < 6 {
		jsonError(w, http.StatusBadRequest, "Username min 2 chars, password min 6 chars")
		return
	}

	user, token, err := h.auth.Setup(body.Username, body.Password)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	isSecure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})

	h.logAudit(r, user.ID, user.Username, "auth.setup", "system", "success", "")

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

func (h *Handler) AuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	user, token, err := h.auth.Login(body.Username, body.Password)
	if err != nil {
		h.logAudit(r, 0, body.Username, "auth.login", "user:"+body.Username, "failure", "invalid credentials")
		jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}

	isSecure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})

	h.logAudit(r, user.ID, user.Username, "auth.login", "user:"+user.Username, "success", "")

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

func (h *Handler) AuthLogout(w http.ResponseWriter, r *http.Request) {
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		_ = h.auth.Logout(cookie.Value)
	}

	isSecure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		MaxAge:   -1,
	})

	h.logAudit(r, actorID, username, "auth.logout", "user", "success", "")

	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Config ──

func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"version":       version.Version,
		"commit":        version.Commit,
		"router_target": h.routerTarget,
	})
}

// ── Profile ──

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		jsonError(w, http.StatusUnauthorized, "Sign in required")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"user": user,
	})
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		jsonError(w, http.StatusUnauthorized, "Sign in required")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	updated, err := h.auth.UpdateProfile(user.ID, body.DisplayName)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"user":   updated,
	})
}

func (h *Handler) SetRecoveryQuestion(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		jsonError(w, http.StatusUnauthorized, "Sign in required")
		return
	}
	var body struct {
		Question        string `json:"question"`
		Answer          string `json:"answer"`
		CurrentPassword string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if strings.TrimSpace(body.Question) == "" {
		jsonError(w, http.StatusBadRequest, "Recovery question cannot be empty")
		return
	}
	if strings.TrimSpace(body.Answer) == "" {
		jsonError(w, http.StatusBadRequest, "Recovery answer cannot be empty")
		return
	}
	if h.auth.IsAuthEnabled() {
		if _, _, err := h.auth.Login(user.Username, body.CurrentPassword); err != nil {
			jsonError(w, http.StatusBadRequest, "Current password incorrect")
			return
		}
	}
	if err := h.auth.SetRecoveryQuestion(user.ID, body.Question, body.Answer); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":            "ok",
		"has_recovery":      true,
		"recovery_question": strings.TrimSpace(body.Question),
	})
}

func (h *Handler) GetRecoveryQuestion(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		jsonError(w, http.StatusBadRequest, "Username is required")
		return
	}
	q, err := h.auth.GetRecoveryQuestion(username)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"username": username,
		"question": q,
	})
}

func (h *Handler) RecoverPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		Answer      string `json:"answer"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if strings.TrimSpace(body.Username) == "" || strings.TrimSpace(body.Answer) == "" {
		jsonError(w, http.StatusBadRequest, "Username and recovery answer are required")
		return
	}
	if len(body.NewPassword) < 6 {
		jsonError(w, http.StatusBadRequest, "New password must be at least 6 characters")
		return
	}
	if err := h.auth.RecoverPassword(body.Username, body.Answer, body.NewPassword); err != nil {
		h.logAudit(r, 0, body.Username, "auth.password.recover", "user:"+body.Username, "failure", err.Error())
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logAudit(r, 0, body.Username, "auth.password.recover", "user:"+body.Username, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Password updated successfully. You can now log in.",
	})
}

func (h *Handler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		jsonError(w, http.StatusUnauthorized, "Sign in required")
		return
	}
	var body struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	// Verify current password first
	if _, _, err := h.auth.Login(user.Username, body.Current); err != nil {
		jsonError(w, http.StatusBadRequest, "Current password incorrect")
		return
	}
	if err := h.auth.ChangePassword(user.ID, body.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logAudit(r, user.ID, user.Username, "auth.password.update", fmt.Sprintf("user:%d", user.ID), "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Users Management ──

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can manage users")
		return
	}
	users, err := h.auth.ListUsers()
	if err != nil {
		slog.Error("failed to list users", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve users")
		return
	}
	jsonResponse(w, http.StatusOK, users)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can manage users")
		return
	}
	var body struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	created, err := h.auth.CreateUser(body.Username, body.DisplayName, body.Password, body.Role)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logAudit(r, user.ID, user.Username, "user.create", "user:"+body.Username, "success", "")
	jsonResponse(w, http.StatusOK, created)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can manage users")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	if user.ID == id {
		jsonError(w, http.StatusBadRequest, "Cannot delete your own account")
		return
	}
	if err := h.auth.DeleteUser(id); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logAudit(r, user.ID, user.Username, "user.delete", "user:"+idStr, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can manage users")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	if err := h.auth.ResetUserPassword(id, body.Password); err != nil {
		if strings.Contains(err.Error(), "cannot reset") {
			jsonError(w, http.StatusForbidden, err.Error())
			return
		}
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logAudit(r, user.ID, user.Username, "user.reset_password", "user:"+idStr, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok", "message": "Password reset successfully"})
}

func (h *Handler) ResetUserRecovery(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can manage users")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	if err := h.auth.ResetUserRecovery(id); err != nil {
		if strings.Contains(err.Error(), "cannot reset") {
			jsonError(w, http.StatusForbidden, err.Error())
			return
		}
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logAudit(r, user.ID, user.Username, "user.reset_recovery", "user:"+idStr, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok", "message": "Recovery question reset successfully"})
}

func (h *Handler) ResetUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Role != "admin" {
		jsonError(w, http.StatusForbidden, "Only administrators can manage users")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	var body struct {
		Password      string `json:"password"`
		ResetRecovery bool   `json:"reset_recovery"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	if body.Password == "" && !body.ResetRecovery {
		jsonError(w, http.StatusBadRequest, "No reset action specified")
		return
	}
	if body.Password != "" {
		if err := h.auth.ResetUserPassword(id, body.Password); err != nil {
			if strings.Contains(err.Error(), "cannot reset") {
				jsonError(w, http.StatusForbidden, err.Error())
				return
			}
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.logAudit(r, user.ID, user.Username, "user.reset_password", "user:"+idStr, "success", "")
	}
	if body.ResetRecovery {
		if err := h.auth.ResetUserRecovery(id); err != nil {
			if strings.Contains(err.Error(), "cannot reset") {
				jsonError(w, http.StatusForbidden, err.Error())
				return
			}
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.logAudit(r, user.ID, user.Username, "user.reset_recovery", "user:"+idStr, "success", "")
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok", "message": "User reset successfully"})
}

// ── Models Handlers ──

func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	providerFilter := r.URL.Query().Get("provider")
	list, err := h.models.ListModels(providerFilter)
	if err != nil {
		slog.Error("failed to list models", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve models")
		return
	}
	jsonResponse(w, http.StatusOK, list)
}

func (h *Handler) ToggleModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ModelID string `json:"model"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	if err := h.models.SetModelEnabled(body.ModelID, body.Enabled); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "model.toggle", body.ModelID, "success", fmt.Sprintf("enabled=%v", body.Enabled))
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SyncModels(w http.ResponseWriter, r *http.Request) {
	added, removed, err := h.models.SyncFromProviders(r.Context(), h.providers)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"added":   added,
		"removed": removed,
	})
}

func (h *Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "Model ID is required")
		return
	}
	if err := h.models.DeleteModel(id); err != nil {
		slog.Error("failed to delete model", "id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to delete model")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "model.delete", id, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemovedModelsSummary serves GET /api/v1/models/removed-summary for the dashboard card.
func (h *Handler) RemovedModelsSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.models.GetRemovedSummary()
	if err != nil {
		slog.Error("failed to build removed models summary", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to load removed models summary")
		return
	}
	jsonResponse(w, http.StatusOK, summary)
}

// ClearRemovedModels serves POST /api/v1/models/clear-removed: hard-deletes all Removed Models.
func (h *Handler) ClearRemovedModels(w http.ResponseWriter, r *http.Request) {
	n, err := h.models.ClearRemovedModels()
	if err != nil {
		slog.Error("failed to clear removed models", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to clear removed models")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "model.clear_removed", "", "success", fmt.Sprintf("deleted=%d", n))
	jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "ok", "deleted": n})
}

// ── Model Groups Handlers ──

func (h *Handler) ListModelGroups(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"groups": []models.ModelGroup{}})
		return
	}
	groups, err := h.models.ListGroups()
	if err != nil {
		slog.Error("failed to list model groups", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve model groups")
		return
	}
	if groups == nil {
		groups = []models.ModelGroup{}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"groups": groups})
}

func (h *Handler) GetModelGroup(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		jsonError(w, http.StatusBadRequest, "Models manager not available")
		return
	}
	id := r.PathValue("id")
	group, err := h.models.GetGroup(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, group)
}

func (h *Handler) CreateModelGroup(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		jsonError(w, http.StatusBadRequest, "Models manager not available")
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Models      []string `json:"models"`
		Priority    int      `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	group, err := h.models.CreateGroupWithPriority(body.Name, body.Description, body.Models, body.Priority)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	if h.keys != nil {
		h.keys.ReloadGroupCache()
	}

	jsonResponse(w, http.StatusCreated, group)
}

func (h *Handler) UpdateModelGroup(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		jsonError(w, http.StatusBadRequest, "Models manager not available")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Models      []string `json:"models"`
		Priority    int      `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	group, err := h.models.UpdateGroupWithPriority(id, body.Name, body.Description, body.Models, body.Priority)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	if h.keys != nil {
		h.keys.ReloadGroupCache()
	}

	jsonResponse(w, http.StatusOK, group)
}

// RemoveUnavailableFromGroup serves POST /api/v1/model-groups/{id}/remove-unavailable.
// It drops entries naming a Removed Model from that one group (ADR 0006).
func (h *Handler) RemoveUnavailableFromGroup(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		jsonError(w, http.StatusBadRequest, "Models manager not available")
		return
	}
	group, removed, err := h.models.RemoveUnavailableEntries(r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.keys != nil {
		h.keys.ReloadGroupCache()
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"group": group, "removed": removed})
}

func (h *Handler) DeleteModelGroup(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		jsonError(w, http.StatusBadRequest, "Models manager not available")
		return
	}
	id := r.PathValue("id")
	if err := h.models.DeleteGroup(id); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	if h.keys != nil {
		h.keys.ReloadGroupCache()
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── System Logs Handlers (Log Explorer) ──

// requestLocation returns the viewer's timezone from the "tz" query parameter
// (IANA name, e.g. "Asia/Jakarta"). Missing or invalid values yield UTC.
func requestLocation(r *http.Request) *time.Location {
	return timeutil.LoadLocation(r.URL.Query().Get("tz"))
}

func parseSyslogFilterParams(q url.Values) syslog.FilterParams {
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	startDate := q.Get("start")
	if startDate == "" {
		startDate = q.Get("start_date")
	}
	endDate := q.Get("end")
	if endDate == "" {
		endDate = q.Get("end_date")
	}

	search := q.Get("search")
	if search == "" {
		search = q.Get("q")
	}

	return syslog.FilterParams{
		Period:    q.Get("period"),
		StartDate: startDate,
		EndDate:   endDate,
		From:      q.Get("from"),
		To:        q.Get("to"),
		Source:    q.Get("source"),
		Level:     q.Get("level"),
		Search:    search,
		Cursor:    q.Get("cursor"),
		Limit:     limit,
		Offset:    offset,
		Loc:       timeutil.LoadLocation(q.Get("tz")),
	}
}

func (h *Handler) GetSystemLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("export") != "" || (q.Get("format") != "" && (q.Get("format") == "csv" || q.Get("format") == "json")) {
		h.ExportSystemLogs(w, r)
		return
	}

	params := parseSyslogFilterParams(q)
	logs, total, err := h.syslog.QueryLogs(params)
	if err != nil {
		slog.Error("failed to query system logs", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to query system logs")
		return
	}

	nextCursor := ""
	if len(logs) > 0 && len(logs) >= params.Limit {
		nextCursor = strconv.FormatInt(logs[len(logs)-1].ID, 10)
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"entries":     logs,
		"logs":        logs,
		"total":       total,
		"next_cursor": nextCursor,
	})
}

func (h *Handler) GetSystemLogVolume(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	buckets, _ := strconv.Atoi(q.Get("buckets"))
	if buckets <= 0 {
		buckets = 60
	}
	params := parseSyslogFilterParams(q)
	vol, err := h.syslog.GetVolume(params, buckets)
	if err != nil {
		slog.Error("failed to get system log volume", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve system log volume")
		return
	}
	jsonResponse(w, http.StatusOK, vol)
}

func (h *Handler) GetSystemLogSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.syslog.GetSources()
	if err != nil {
		slog.Error("failed to get system log sources", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve system log sources")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"sources": sources,
	})
}

func (h *Handler) ExportSystemLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := strings.ToLower(q.Get("format"))
	if format == "" {
		format = strings.ToLower(q.Get("export"))
	}
	if format != "csv" && format != "json" {
		format = "csv"
	}

	params := parseSyslogFilterParams(q)
	params.Limit = 5000
	params.Offset = 0

	logs, _, err := h.syslog.QueryLogs(params)
	if err != nil {
		slog.Error("failed to export system logs", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to export system logs")
		return
	}

	filename := fmt.Sprintf("nineguard-system-logs-%s.%s", time.Now().UTC().Format("20060102-150405"), format)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"timestamp", "level", "source", "message", "attrs"})
		for _, e := range logs {
			_ = cw.Write([]string{
				e.Timestamp.Format(time.RFC3339Nano),
				e.Level,
				e.Source,
				e.Message,
				e.Attrs,
			})
		}
		cw.Flush()
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(logs)
	}
}

// ── Traffic Handlers ──

func parseFilterParams(q url.Values) traffic.FilterParams {
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	startDate := q.Get("start")
	if startDate == "" {
		startDate = q.Get("start_date")
	}
	endDate := q.Get("end")
	if endDate == "" {
		endDate = q.Get("end_date")
	}

	search := q.Get("search")
	if search == "" {
		search = q.Get("q")
	}

	apiKey := q.Get("api_key")
	if apiKey == "" {
		apiKey = q.Get("key")
	}

	clientIP := q.Get("client_ip")
	if clientIP == "" {
		clientIP = q.Get("ip")
	}

	var hasImages *bool
	if imgParam := q.Get("has_images"); imgParam != "" {
		val := (imgParam == "1" || strings.EqualFold(imgParam, "true"))
		hasImages = &val
	}

	return traffic.FilterParams{
		Period:    q.Get("period"),
		StartDate: startDate,
		EndDate:   endDate,
		From:      q.Get("from"),
		To:        q.Get("to"),
		Model:     q.Get("model"),
		APIKey:    apiKey,
		APIKeyID:  q.Get("key_id"),
		Provider:  q.Get("provider"),
		ClientIP:  clientIP,
		Status:    q.Get("status"),
		Level:     q.Get("level"),
		Search:    search,
		Cursor:    q.Get("cursor"),
		Limit:     limit,
		Offset:    offset,
		Loc:       timeutil.LoadLocation(q.Get("tz")),
		HasImages: hasImages,
	}
}

func (h *Handler) GetTrafficLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("export") != "" || (q.Get("format") != "" && (q.Get("format") == "csv" || q.Get("format") == "json")) {
		h.ExportTrafficLogs(w, r)
		return
	}

	params := parseFilterParams(q)
	logs, total, err := h.traffic.QueryLogs(params)
	if err != nil {
		slog.Error("failed to query traffic logs", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to query traffic logs")
		return
	}

	nextCursor := ""
	if len(logs) > 0 && len(logs) >= params.Limit {
		nextCursor = strconv.FormatInt(logs[len(logs)-1].ID, 10)
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"entries":     logs,
		"logs":        logs,
		"total":       total,
		"next_cursor": nextCursor,
	})
}

func (h *Handler) GetTrafficVolume(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	buckets, _ := strconv.Atoi(q.Get("buckets"))
	if buckets <= 0 {
		buckets = 60
	}
	params := parseFilterParams(q)
	vol, err := h.traffic.GetVolume(params, buckets)
	if err != nil {
		slog.Error("failed to get traffic volume", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve traffic volume")
		return
	}
	jsonResponse(w, http.StatusOK, vol)
}

func (h *Handler) ExportTrafficLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := strings.ToLower(q.Get("format"))
	if format == "" {
		format = strings.ToLower(q.Get("export"))
	}
	if format != "csv" && format != "json" {
		format = "csv"
	}

	params := parseFilterParams(q)
	params.Limit = 5000
	params.Offset = 0

	logs, _, err := h.traffic.QueryLogs(params)
	if err != nil {
		slog.Error("failed to export traffic logs", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to export traffic logs")
		return
	}

	filename := fmt.Sprintf("nineguard-traffic-%s.%s", time.Now().UTC().Format("20060102-150405"), format)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{
			"timestamp", "level", "status_code", "model", "provider", "client_key_name", "client_key",
			"duration_ms", "prompt_tokens", "completion_tokens", "total_tokens", "stream", "client_ip", "error_message", "message",
		})
		for _, e := range logs {
			streamStr := "false"
			if e.Stream {
				streamStr = "true"
			}
			errStr := ""
			if e.ErrorMessage != nil {
				errStr = *e.ErrorMessage
			}
			_ = cw.Write([]string{
				e.Timestamp.Format(time.RFC3339Nano),
				e.Level,
				strconv.Itoa(e.StatusCode),
				e.Model,
				e.ProviderID,
				e.APIKeyName,
				e.APIKey,
				strconv.Itoa(e.DurationMs),
				strconv.Itoa(e.PromptTokens),
				strconv.Itoa(e.CompletionTokens),
				strconv.Itoa(e.TotalTokens),
				streamStr,
				e.ClientIP,
				errStr,
				e.Message,
			})
		}
		cw.Flush()
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(logs)
	}
}

func (h *Handler) GetTrafficStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period := q.Get("period")
	startDate := q.Get("start")
	if startDate == "" {
		startDate = q.Get("start_date")
	}
	endDate := q.Get("end")
	if endDate == "" {
		endDate = q.Get("end_date")
	}
	stats, err := h.traffic.GetDashboardStats(period, startDate, endDate, requestLocation(r))
	if err != nil {
		slog.Error("failed to get traffic stats", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve traffic statistics")
		return
	}
	jsonResponse(w, http.StatusOK, stats)
}

func (h *Handler) GetUsageReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period := q.Get("period")
	startDate := q.Get("start")
	if startDate == "" {
		startDate = q.Get("start_date")
	}
	endDate := q.Get("end")
	if endDate == "" {
		endDate = q.Get("end_date")
	}
	report, err := h.traffic.GetUsageReports(period, startDate, endDate, requestLocation(r))
	if err != nil {
		slog.Error("failed to get usage report", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve usage report")
		return
	}
	jsonResponse(w, http.StatusOK, report)
}

// ── API Keys Handlers ──

// ── API Keys & Upstream Settings Handlers ──

// ListKeys serves GET /api/v1/keys. Without "page" it returns every key
// ({"keys": [...]}, legacy shape used by dropdowns and scripts). With "page"
// it returns one page: {"keys", "total", "page", "limit"}.
func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"keys": []keys.KeyInfo{}})
		return
	}
	q := r.URL.Query()
	if !q.Has("page") {
		list, err := h.keys.ListKeys()
		if err != nil {
			slog.Error("failed to list keys", "error", err)
			jsonError(w, http.StatusInternalServerError, "Failed to retrieve API keys")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"keys": list})
		return
	}

	pageNum, err := strconv.Atoi(q.Get("page"))
	if err != nil || pageNum < 1 {
		jsonError(w, http.StatusBadRequest, "invalid page: must be an integer >= 1")
		return
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid limit: must be an integer")
			return
		}
	}
	opts := keys.ListOptions{
		Page:   pageNum,
		Limit:  limit,
		Sort:   q.Get("sort"),
		Order:  q.Get("order"),
		Query:  q.Get("q"),
		Status: q.Get("status"),
		Mode:   q.Get("mode"),
	}
	if err := opts.Normalize(); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	pageRes, err := h.keys.ListKeysPage(opts)
	if err != nil {
		slog.Error("failed to list keys page", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve API keys")
		return
	}
	jsonResponse(w, http.StatusOK, pageRes)
}

func isValidQuotaPeriod(p string) bool {
	switch p {
	case "none", "daily", "weekly", "monthly", "total":
		return true
	default:
		return false
	}
}

func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	var body struct {
		Name            string   `json:"name"`
		ModelAccessMode string   `json:"model_access_mode"`
		ModelGroupIDs   []string `json:"model_group_ids"`
		AllowedModels   []string `json:"allowed_models"`
		QuotaLimit      int64    `json:"quota_limit"`
		QuotaPeriod     string   `json:"quota_period"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.QuotaLimit < 0 {
		jsonError(w, http.StatusBadRequest, "Quota limit cannot be negative")
		return
	}
	body.QuotaPeriod = strings.ToLower(strings.TrimSpace(body.QuotaPeriod))
	if body.QuotaPeriod == "" {
		body.QuotaPeriod = "none"
	}
	if !isValidQuotaPeriod(body.QuotaPeriod) {
		jsonError(w, http.StatusBadRequest, "Invalid quota period: must be none, daily, weekly, monthly, or total")
		return
	}

	keyInfo, err := h.keys.CreateKeyWithOptions(body.Name, keys.CreateKeyOptions{
		ModelAccessMode: body.ModelAccessMode,
		ModelGroupIDs:   body.ModelGroupIDs,
		AllowedModels:   body.AllowedModels,
		QuotaLimit:      body.QuotaLimit,
		QuotaPeriod:     body.QuotaPeriod,
	})
	if err != nil {
		slog.Error("failed to create key", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "key.create", keyInfo.ID, "success", "name="+keyInfo.Name)
	jsonResponse(w, http.StatusOK, keyInfo)
}

func (h *Handler) UpdateKey(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Name            string   `json:"name"`
		ModelAccessMode string   `json:"model_access_mode"`
		ModelGroupIDs   []string `json:"model_group_ids"`
		AllowedModels   []string `json:"allowed_models"`
		QuotaLimit      *int64   `json:"quota_limit"`
		QuotaPeriod     *string  `json:"quota_period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if body.QuotaLimit != nil && *body.QuotaLimit < 0 {
		jsonError(w, http.StatusBadRequest, "Quota limit cannot be negative")
		return
	}
	if body.QuotaPeriod != nil {
		p := strings.ToLower(strings.TrimSpace(*body.QuotaPeriod))
		if p == "" {
			p = "none"
		}
		if !isValidQuotaPeriod(p) {
			jsonError(w, http.StatusBadRequest, "Invalid quota period: must be none, daily, weekly, monthly, or total")
			return
		}
		*body.QuotaPeriod = p
	}

	keyInfo, err := h.keys.UpdateKey(id, body.Name, body.ModelAccessMode, body.ModelGroupIDs, body.AllowedModels)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.QuotaLimit != nil || body.QuotaPeriod != nil {
		qLimit := keyInfo.QuotaLimit
		if body.QuotaLimit != nil {
			qLimit = *body.QuotaLimit
		}
		qPeriod := keyInfo.QuotaPeriod
		if body.QuotaPeriod != nil {
			qPeriod = *body.QuotaPeriod
		}
		keyInfo, err = h.keys.UpdateKeyQuota(id, qLimit, qPeriod)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "key.update", id, "success", "")
	jsonResponse(w, http.StatusOK, keyInfo)
}

func (h *Handler) ToggleKey(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if err := h.keys.ToggleKey(id, body.Active); err != nil {
		slog.Error("failed to toggle key", "id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to toggle API key")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "key.toggle", id, "success", fmt.Sprintf("active=%v", body.Active))
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	id := r.PathValue("id")
	if err := h.keys.DeleteKey(id); err != nil {
		slog.Error("failed to delete key", "id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to delete API key")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "key.delete", id, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) GetKeyVelocity(w http.ResponseWriter, r *http.Request) {
	if h.traffic == nil {
		jsonError(w, http.StatusBadRequest, "Traffic manager not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "Key ID required")
		return
	}

	stats, err := h.traffic.GetVelocityStats(id, time.Now())
	if err != nil {
		slog.Error("failed to get key velocity stats", "id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve velocity stats")
		return
	}
	jsonResponse(w, http.StatusOK, stats)
}

// ── Providers Handlers ──

func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"providers": []interface{}{}})
		return
	}
	list, err := h.providers.ListProviders()
	if err != nil {
		slog.Error("failed to list providers", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve providers")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"providers": list})
}

func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	var body struct {
		Name                    string `json:"name"`
		Route                   string `json:"route"`
		APIKey                  string `json:"api_key"`
		Prefix                  string `json:"prefix"`
		IsDefault               bool   `json:"is_default"`
		IsActive                bool   `json:"is_active"`
		UpstreamTokenSaving     bool   `json:"upstream_token_saving"`
		UpstreamTokenSavingNote string `json:"upstream_token_saving_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	p, err := h.providers.CreateProviderWithTokenSaving(body.Name, body.Route, body.APIKey, body.Prefix, body.IsDefault, body.IsActive, body.UpstreamTokenSaving, body.UpstreamTokenSavingNote)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "provider.create", p.ID, "success", "name="+p.Name)
	jsonResponse(w, http.StatusOK, p)
}

func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Name                    string  `json:"name"`
		Route                   string  `json:"route"`
		APIKey                  *string `json:"api_key"`
		Prefix                  string  `json:"prefix"`
		IsDefault               bool    `json:"is_default"`
		IsActive                bool    `json:"is_active"`
		UpstreamTokenSaving     bool    `json:"upstream_token_saving"`
		UpstreamTokenSavingNote string  `json:"upstream_token_saving_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	apiKey := ""
	if body.APIKey != nil {
		apiKey = *body.APIKey
	} else if existing, err := h.providers.GetProvider(id); err == nil && existing != nil {
		apiKey = existing.APIKey
	}

	p, err := h.providers.UpdateProviderWithTokenSaving(id, body.Name, body.Route, apiKey, body.Prefix, body.IsDefault, body.IsActive, body.UpstreamTokenSaving, body.UpstreamTokenSavingNote)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "provider.update", id, "success", "")
	jsonResponse(w, http.StatusOK, p)
}

func (h *Handler) ToggleProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if err := h.providers.ToggleProvider(id, body.Active); err != nil {
		slog.Error("failed to toggle provider", "id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to toggle provider")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "provider.toggle", id, "success", fmt.Sprintf("active=%v", body.Active))
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SetDefaultProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	id := r.PathValue("id")
	if err := h.providers.SetDefaultProvider(id); err != nil {
		slog.Error("failed to set default provider", "id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to set default provider")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "provider.set_default", id, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	id := r.PathValue("id")
	if err := h.providers.DeleteProvider(id); err != nil {
		slog.Error("failed to delete provider", "id", id, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to delete provider")
		return
	}
	var actorID int64
	var username string
	if u := auth.UserFromContext(r.Context()); u != nil {
		actorID = u.ID
		username = u.Username
	}
	h.logAudit(r, actorID, username, "provider.delete", id, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) TestProviderConnection(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	var body struct {
		ID     string `json:"id"`
		Route  string `json:"route"`
		APIKey string `json:"api_key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.APIKey == "" && body.ID != "" {
		if p, err := h.providers.GetProvider(body.ID); err == nil && p != nil {
			body.APIKey = p.APIKey
		}
	}

	res, err := h.providers.TestProvider(r.Context(), body.Route, body.APIKey)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, res)
}

func (h *Handler) GetUpstreamSettings(w http.ResponseWriter, r *http.Request) {
	var info map[string]interface{}
	if h.keys != nil {
		info = h.keys.GetUpstreamInfo()
	} else {
		info = map[string]interface{}{"configured": false, "masked_key": "", "key": ""}
	}
	info["router_target"] = h.routerTarget
	jsonResponse(w, http.StatusOK, info)
}

func (h *Handler) SetUpstreamSettings(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	var body struct {
		RouterAPIKey string `json:"router_api_key"`
		RouterTarget string `json:"router_target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if target := strings.TrimSpace(body.RouterTarget); target != "" {
		target = strings.TrimRight(target, "/")
		h.routerTarget = target
		_ = h.keys.SetUpstreamTarget(target)
	}

	if key := strings.TrimSpace(body.RouterAPIKey); key != "" {
		if err := h.keys.SetUpstreamKey(key); err != nil {
			slog.Error("failed to set upstream key", "error", err)
			jsonError(w, http.StatusInternalServerError, "Failed to configure upstream key")
			return
		}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":        "ok",
		"router_target": h.routerTarget,
	})
}

func (h *Handler) TestUpstreamConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RouterAPIKey string `json:"router_api_key"`
		RouterTarget string `json:"router_target"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	target := h.routerTarget
	if strings.TrimSpace(body.RouterTarget) != "" {
		target = strings.TrimRight(strings.TrimSpace(body.RouterTarget), "/")
	}

	key := strings.TrimSpace(body.RouterAPIKey)
	if key == "" && h.keys != nil {
		key = h.keys.GetUpstreamKey()
	}

	if key == "" {
		jsonError(w, http.StatusBadRequest, "No 9router API key provided or configured")
		return
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(r.Context(), "GET", target+"/v1/models", nil)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":         false,
			"error":      fmt.Sprintf("Cannot reach 9router at %s: %v", target, err),
			"latency_ms": latency,
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":         false,
			"status":     401,
			"error":      "9router rejected the key: HTTP 401 Unauthorized (Invalid API Key)",
			"latency_ms": latency,
		})
		return
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)

	// Secondary check: verify key authorization on chat completions
	chatCheckReq, err := http.NewRequestWithContext(r.Context(), "POST", target+"/v1/chat/completions", strings.NewReader(`{"model":"__probe__"}`))
	if err == nil {
		chatCheckReq.Header.Set("Authorization", "Bearer "+key)
		chatCheckReq.Header.Set("Content-Type", "application/json")
		chatResp, chatErr := client.Do(chatCheckReq)
		if chatErr == nil {
			defer chatResp.Body.Close()
			if chatResp.StatusCode == http.StatusUnauthorized {
				jsonResponse(w, http.StatusOK, map[string]interface{}{
					"ok":         false,
					"status":     401,
					"error":      "9router rejected this key (HTTP 401 Unauthorized: Invalid API Key)",
					"latency_ms": latency,
				})
				return
			}
		}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"status":       200,
		"models_count": len(payload.Data),
		"latency_ms":   latency,
		"target":       target,
	})
}

// ── Traffic & Spike Settings (ADR 0005) ──

func (h *Handler) GetTrafficSettings(w http.ResponseWriter, r *http.Request) {
	threshold := 8000
	recMode := "disabled"
	if h.traffic != nil {
		threshold = h.traffic.GetHeavyTokenThreshold()
		recMode = h.traffic.GetRecordPayloadsSetting()
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"heavy_token_threshold": threshold,
		"record_payloads":       recMode,
	})
}

func (h *Handler) SetTrafficSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HeavyTokenThreshold int    `json:"heavy_token_threshold"`
		RecordPayloads      string `json:"record_payloads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	if body.HeavyTokenThreshold > 0 && h.traffic != nil {
		if err := h.traffic.SetHeavyTokenThreshold(body.HeavyTokenThreshold); err != nil {
			slog.Error("failed to set heavy token threshold", "error", err)
			jsonError(w, http.StatusInternalServerError, "Failed to save traffic settings")
			return
		}
	}
	if body.RecordPayloads != "" && h.traffic != nil {
		mode := strings.ToLower(strings.TrimSpace(body.RecordPayloads))
		if mode != "disabled" && mode != "errors_only" && mode != "all" {
			jsonError(w, http.StatusBadRequest, "Invalid record_payloads: must be disabled, errors_only, or all")
			return
		}
		if err := h.traffic.SetRecordPayloadsSetting(mode); err != nil {
			slog.Error("failed to set record payloads setting", "error", err)
			jsonError(w, http.StatusInternalServerError, "Failed to save traffic settings")
			return
		}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":                "ok",
		"heavy_token_threshold": h.traffic.GetHeavyTokenThreshold(),
		"record_payloads":       h.traffic.GetRecordPayloadsSetting(),
	})
}

func (h *Handler) GetTrafficPayload(w http.ResponseWriter, r *http.Request) {
	if h.traffic == nil {
		jsonError(w, http.StatusBadRequest, "Traffic manager not available")
		return
	}
	idStr := r.PathValue("id")
	trafficID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid traffic ID")
		return
	}
	payload, err := h.traffic.GetPayload(trafficID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Payload not found or not recorded")
		return
	}
	jsonResponse(w, http.StatusOK, payload)
}

