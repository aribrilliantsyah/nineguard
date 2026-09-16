package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/auth"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/syslog"
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
	proxy        *proxy.Proxy
	routerTarget string
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

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})

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
		jsonError(w, http.StatusUnauthorized, err.Error())
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

func (h *Handler) AuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		_ = h.auth.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
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
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
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
		jsonError(w, http.StatusInternalServerError, err.Error())
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
	if err := h.auth.DeleteUser(id); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Models Handlers ──

func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	providerFilter := r.URL.Query().Get("provider")
	list, err := h.models.ListModels(providerFilter)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
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
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Model Groups Handlers ──

func (h *Handler) ListModelGroups(w http.ResponseWriter, r *http.Request) {
	if h.models == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"groups": []models.ModelGroup{}})
		return
	}
	groups, err := h.models.ListGroups()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	group, err := h.models.CreateGroup(body.Name, body.Description, body.Models)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	group, err := h.models.UpdateGroup(id, body.Name, body.Description, body.Models)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	if h.keys != nil {
		h.keys.ReloadGroupCache()
	}

	jsonResponse(w, http.StatusOK, group)
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
		jsonError(w, http.StatusInternalServerError, err.Error())
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
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, vol)
}

func (h *Handler) GetSystemLogSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.syslog.GetSources()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
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
		jsonError(w, http.StatusInternalServerError, err.Error())
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

	return traffic.FilterParams{
		Period:    q.Get("period"),
		StartDate: startDate,
		EndDate:   endDate,
		From:      q.Get("from"),
		To:        q.Get("to"),
		Model:     q.Get("model"),
		APIKey:    apiKey,
		Provider:  q.Get("provider"),
		ClientIP:  clientIP,
		Status:    q.Get("status"),
		Level:     q.Get("level"),
		Search:    search,
		Cursor:    q.Get("cursor"),
		Limit:     limit,
		Offset:    offset,
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
		jsonError(w, http.StatusInternalServerError, err.Error())
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
		jsonError(w, http.StatusInternalServerError, err.Error())
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
		jsonError(w, http.StatusInternalServerError, err.Error())
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
	stats, err := h.traffic.GetDashboardStats(period, startDate, endDate)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
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
	report, err := h.traffic.GetUsageReports(period, startDate, endDate)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, report)
}

// ── API Keys Handlers ──

// ── API Keys & Upstream Settings Handlers ──

func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"keys": []keys.KeyInfo{}})
		return
	}
	list, err := h.keys.ListKeys()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"keys": list})
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
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	keyInfo, err := h.keys.CreateKey(body.Name, body.ModelAccessMode, body.ModelGroupIDs, body.AllowedModels)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	keyInfo, err := h.keys.UpdateKey(id, body.Name, body.ModelAccessMode, body.ModelGroupIDs, body.AllowedModels)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
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
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteKey(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	id := r.PathValue("id")
	if err := h.keys.DeleteKey(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Providers Handlers ──

func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"providers": []interface{}{}})
		return
	}
	list, err := h.providers.ListProviders()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
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
		Name      string `json:"name"`
		Route     string `json:"route"`
		APIKey    string `json:"api_key"`
		Prefix    string `json:"prefix"`
		IsDefault bool   `json:"is_default"`
		IsActive  bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	p, err := h.providers.CreateProvider(body.Name, body.Route, body.APIKey, body.Prefix, body.IsDefault, body.IsActive)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, p)
}

func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	id := r.PathValue("id")
	var body struct {
		Name      string `json:"name"`
		Route     string `json:"route"`
		APIKey    string `json:"api_key"`
		Prefix    string `json:"prefix"`
		IsDefault bool   `json:"is_default"`
		IsActive  bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	p, err := h.providers.UpdateProvider(id, body.Name, body.Route, body.APIKey, body.Prefix, body.IsDefault, body.IsActive)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
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
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SetDefaultProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	id := r.PathValue("id")
	if err := h.providers.SetDefaultProvider(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	id := r.PathValue("id")
	if err := h.providers.DeleteProvider(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) TestProviderConnection(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	var body struct {
		Route  string `json:"route"`
		APIKey string `json:"api_key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

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
			jsonError(w, http.StatusInternalServerError, err.Error())
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
