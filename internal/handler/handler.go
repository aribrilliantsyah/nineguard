package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/auth"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
	"nineguard/internal/version"
)

type Handler struct {
	auth         *auth.Manager
	models       *models.Manager
	traffic      *traffic.Manager
	keys         *keys.Manager
	providers    *providers.Manager
	proxy        *proxy.Proxy
	routerTarget string
}

func New(am *auth.Manager, mm *models.Manager, tm *traffic.Manager, km *keys.Manager, pm *providers.Manager, pr *proxy.Proxy, target string) *Handler {
	return &Handler{
		auth:         am,
		models:       mm,
		traffic:      tm,
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

// ── Traffic Handlers ──

func (h *Handler) GetTrafficLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
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

	params := traffic.FilterParams{
		Period:    q.Get("period"),
		StartDate: startDate,
		EndDate:   endDate,
		Model:     q.Get("model"),
		APIKey:    q.Get("api_key"),
		Status:    q.Get("status"),
		Limit:     limit,
		Offset:    offset,
	}

	logs, total, err := h.traffic.QueryLogs(params)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
		"total": total,
	})
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
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	keyInfo, err := h.keys.CreateKey(body.Name)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
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
		h.models.SetRouterTarget(target)
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
