package traffic

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/timeutil"
)

type LogEntry struct {
	ID               int64     `json:"id"`
	Timestamp        time.Time `json:"timestamp"`
	APIKey           string    `json:"api_key"`
	APIKeyName       string    `json:"api_key_name"`
	APIKeyID         string    `json:"api_key_id,omitempty"`
	ProviderID       string    `json:"provider_id,omitempty"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	DurationMs       int       `json:"duration_ms"`
	StatusCode       int       `json:"status_code"`
	ClientIP         string    `json:"client_ip"`
	Stream           bool      `json:"stream"`
	ErrorMessage     *string   `json:"error_message,omitempty"`
	Level            string    `json:"level"`
	Message          string    `json:"message"`
	PluginsSkipped   string    `json:"plugins_skipped"`
	PluginsApplied   string    `json:"plugins_applied"`
	TokensSaved      int       `json:"tokens_saved"`
	TokensOverhead   int       `json:"tokens_overhead"`
	PluginErrors     string    `json:"plugin_errors"`
	PluginMs         int       `json:"plugin_ms"`
	HasImages        bool      `json:"has_images"`
	ImageCount       int       `json:"image_count"`
	// ModelRemoved is true when the model of this row is a Removed Model (ADR 0006).
	ModelRemoved bool `json:"model_removed"`
}

type FilterParams struct {
	Period    string // "today", "yesterday", "7d", "30d", "month", "last_month", "all", "custom"
	StartDate string // "YYYY-MM-DD"
	EndDate   string // "YYYY-MM-DD"
	From      string // RFC3339 / ISO or epoch
	To        string // RFC3339 / ISO or epoch
	Model     string
	APIKey    string
	Provider  string
	ClientIP  string
	Status    string // "ok", "blocked", "error" or status code
	Level     string // comma-separated: "DEBUG,INFO,WARN,ERROR,FATAL"
	Search    string // search term / query string
	Limit     int
	Offset    int
	Cursor    string         // id cursor for pagination
	APIKeyID  string         // exact traffic_logs.api_key_id match
	Loc       *time.Location // viewer timezone for Period/StartDate/EndDate; nil = UTC
	HasImages *bool          // filter multimodal requests
}

type VolumeBucket struct {
	Start  int64            `json:"start"` // unix nano
	Counts map[string]int64 `json:"counts"`
}

type VolumeResult struct {
	From        int64            `json:"from"` // unix nano
	To          int64            `json:"to"`   // unix nano
	BucketNanos int64            `json:"bucket_nanos"`
	Buckets     []VolumeBucket   `json:"buckets"`
	Totals      map[string]int64 `json:"totals"`
}

type ModelStat struct {
	Model    string  `json:"model"`
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	Share    float64 `json:"share"` // percentage
	Trend    []int   `json:"trend,omitempty"`
}

type KeyStat struct {
	KeyID    string  `json:"key_id,omitempty"` // empty for unlinked / deleted keys
	Unlinked bool    `json:"unlinked"`
	Name     string  `json:"name"`
	Key      string  `json:"key"`
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	Share    float64 `json:"share"`
	Trend    []int   `json:"trend,omitempty"`
}

type KeyUsageTrendPoint struct {
	Time     string `json:"time"`
	KeyID    string `json:"key_id,omitempty"`
	KeyName  string `json:"key_name"`
	Key      string `json:"key"`
	Requests int    `json:"requests"`
	Tokens   int    `json:"tokens"`
}

type TimeSeriesPoint struct {
	Time         string  `json:"time"`
	Requests     int     `json:"requests"`
	Tokens       int     `json:"tokens"`
	PromptTokens int     `json:"prompt_tokens"`
	CompTokens   int     `json:"comp_tokens"`
	Errors       int     `json:"errors"`
	Blocked      int     `json:"blocked"`
	Success      int     `json:"success"`
	ErrorRate    float64 `json:"error_rate"`
	AvgLatencyMs int     `json:"avg_latency_ms"`
}

type PeriodComparison struct {
	PrevRequests     int     `json:"prev_requests"`
	PrevErrors       int     `json:"prev_errors"`
	PrevBlocked      int     `json:"prev_blocked"`
	PrevTokens       int     `json:"prev_tokens"`
	PrevSuccess      int     `json:"prev_success"`
	PrevErrorRate    float64 `json:"prev_error_rate"`
	ReqDeltaPct      float64 `json:"req_delta_pct"`
	ErrDeltaPct      float64 `json:"err_delta_pct"`
	ErrorRateDeltaPt float64 `json:"error_rate_delta_pt"`
}

type ErrorSourceStat struct {
	Source    string  `json:"source"`
	Namespace string  `json:"namespace"`
	Count     int     `json:"count"`
	Share     float64 `json:"share"`
	Trend     []int   `json:"trend"`
	IsNew     bool    `json:"is_new"`
}

type LevelMixStat struct {
	Level  string  `json:"level"`
	Label  string  `json:"label"`
	Count  int     `json:"count"`
	Share  float64 `json:"share"`
	VsPrev float64 `json:"vs_prev"`
	Color  string  `json:"color"`
}

type DashboardStats struct {
	TotalRequests    int                  `json:"total_requests"`
	TotalTokens      int                  `json:"total_tokens"`
	PromptTokens     int                  `json:"prompt_tokens"`
	CompletionTokens int                  `json:"completion_tokens"`
	TokensSaved      int                  `json:"tokens_saved"`
	BlockedRequests  int                  `json:"blocked_requests"`
	SuccessRequests  int                  `json:"success_requests"`
	ErrorRequests    int                  `json:"error_requests"`
	SuccessRate      float64              `json:"success_rate"`
	AvgDurationMs    int                  `json:"avg_duration_ms"`
	P95DurationMs    int                  `json:"p95_duration_ms"`
	MinDurationMs    int                  `json:"min_duration_ms"`
	MaxDurationMs    int                  `json:"max_duration_ms"`
	StreamRequests   int                  `json:"stream_requests"`
	SyncRequests     int                  `json:"sync_requests"`
	ActiveModels     int                  `json:"active_models"`
	ActiveKeys       int                  `json:"active_keys"`
	ActiveProviders  int                  `json:"active_providers"`
	TopModels        []ModelStat          `json:"top_models"`
	TopKeys          []KeyStat            `json:"top_keys"`
	VolumeSeries     []TimeSeriesPoint    `json:"volume_series"`
	KeyUsageTrends   []KeyUsageTrendPoint `json:"key_usage_trends"`
	ModelMix         []ModelStat          `json:"model_mix"`
	Comparison       PeriodComparison     `json:"comparison"`
	TopErrorSources  []ErrorSourceStat    `json:"top_error_sources"`
	LevelMix         []LevelMixStat       `json:"level_mix"`
}

type ModelUsageSummary struct {
	Model        string  `json:"model"`
	TotalTokens  int     `json:"total_tokens"`
	PromptTokens int     `json:"prompt_tokens"`
	CompTokens   int     `json:"comp_tokens"`
	Requests     int     `json:"requests"`
	Share        float64 `json:"share"`
}

type KeyUsageSummary struct {
	KeyID        string  `json:"key_id,omitempty"`
	Unlinked     bool    `json:"unlinked"`
	KeyName      string  `json:"key_name"`
	Key          string  `json:"key"`
	TotalTokens  int     `json:"total_tokens"`
	PromptTokens int     `json:"prompt_tokens"`
	CompTokens   int     `json:"comp_tokens"`
	Requests     int     `json:"requests"`
	Share        float64 `json:"share"`
}

type KeyUsageBreakdown struct {
	KeyID            string              `json:"key_id,omitempty"` // empty for unlinked / deleted keys
	Unlinked         bool                `json:"unlinked"`         // true: grouped by historical key name
	KeyName          string              `json:"key_name"`
	Key              string              `json:"key"`
	TotalTokens      int                 `json:"total_tokens"`
	PromptTokens     int                 `json:"prompt_tokens"`
	CompletionTokens int                 `json:"completion_tokens"`
	TotalRequests    int                 `json:"total_requests"`
	SuccessRequests  int                 `json:"success_requests"`
	ErrorRequests    int                 `json:"error_requests"`
	BlockedRequests  int                 `json:"blocked_requests"`
	AvgDurationMs    int                 `json:"avg_duration_ms"`
	TokenShare       float64             `json:"token_share"`
	LastActiveAt     *string             `json:"last_active_at,omitempty"`
	TokensSaved      int                 `json:"tokens_saved"`
	ModelUsage       []ModelUsageSummary `json:"model_usage"`
}

type ModelUsageBreakdown struct {
	Model            string            `json:"model"`
	Enabled          bool              `json:"enabled"`
	Removed          bool              `json:"removed"`
	TotalTokens      int               `json:"total_tokens"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	TotalRequests    int               `json:"total_requests"`
	SuccessRequests  int               `json:"success_requests"`
	ErrorRequests    int               `json:"error_requests"`
	BlockedRequests  int               `json:"blocked_requests"`
	AvgDurationMs    int               `json:"avg_duration_ms"`
	TokenShare       float64           `json:"token_share"`
	LastActiveAt     *string           `json:"last_active_at,omitempty"`
	KeyConsumers     []KeyUsageSummary `json:"key_consumers"`
}

type PluginUsageBreakdown struct {
	PluginID    string `json:"plugin_id"`
	Requests    int    `json:"requests"`
	TokensSaved int    `json:"tokens_saved"`
}

type UsageReport struct {
	Period           string                 `json:"period"`
	StartDate        string                 `json:"start_date,omitempty"`
	EndDate          string                 `json:"end_date,omitempty"`
	TotalTokens      int                    `json:"total_tokens"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	TokensSaved      int                    `json:"tokens_saved"`
	TotalRequests    int                    `json:"total_requests"`
	TopConsumerKey   string                 `json:"top_consumer_key"`
	TopModel         string                 `json:"top_model"`
	KeysBreakdown    []KeyUsageBreakdown    `json:"keys_breakdown"`
	ModelsBreakdown  []ModelUsageBreakdown  `json:"models_breakdown"`
	PluginsBreakdown []PluginUsageBreakdown `json:"plugins_breakdown"`
}

func sanitizeDate(d string) string {
	d = strings.TrimSpace(d)
	if len(d) == 10 {
		if _, err := time.Parse("2006-01-02", d); err == nil {
			return d
		}
	}
	return ""
}

// nowFunc is the clock used for period resolution; tests may replace it.
var nowFunc = time.Now

// buildDateFilter returns the WHERE fragment for the current report window,
// with calendar boundaries in loc (nil = UTC).
func buildDateFilter(period, startDate, endDate string, loc *time.Location) (string, []interface{}) {
	cur, _ := timeutil.ResolvePeriod(period, sanitizeDate(startDate), sanitizeDate(endDate), loc, nowFunc())
	return cur.SQL("timestamp")
}

// buildPrevDateFilter returns the WHERE fragment for the window preceding the
// current one, used for period-over-period comparison.
func buildPrevDateFilter(period, startDate, endDate string, loc *time.Location) (string, []interface{}) {
	_, prev := timeutil.ResolvePeriod(period, sanitizeDate(startDate), sanitizeDate(endDate), loc, nowFunc())
	return prev.SQL("timestamp")
}

// Key grouping (ADR 0003). Traffic rows are grouped by API key ID when the
// key still exists; otherwise (NULL api_key_id, or the key was deleted) they
// fall into "unlinked" groups by historical key name. The expressions require
// traffic_logs aliased as t with keyJoin applied.
const (
	keyJoin      = " LEFT JOIN api_keys k ON k.id = t.api_key_id"
	keyGroupExpr = "CASE WHEN k.id IS NOT NULL THEN 'id:' || k.id ELSE 'name:' || COALESCE(NULLIF(t.api_key_name, ''), t.api_key, '') END"
	keyNameExpr  = "CASE WHEN k.id IS NOT NULL THEN k.name ELSE COALESCE(NULLIF(t.api_key_name, ''), t.api_key, '') END"
)

// splitKeyGroup turns a keyGroupExpr value into (key ID, unlinked).
func splitKeyGroup(g string) (string, bool) {
	if strings.HasPrefix(g, "id:") {
		return strings.TrimPrefix(g, "id:"), false
	}
	return "", true
}

type Manager struct {
	db *db.DB
}

func NewManager(database *db.DB) *Manager {
	return &Manager{db: database}
}

func NormalizeLevel(l string) string {
	switch strings.ToUpper(strings.TrimSpace(l)) {
	case "DEBUG", "TRACE", "DBG":
		return "DEBUG"
	case "WARN", "WARNING", "WRN":
		return "WARN"
	case "ERROR", "ERR":
		return "ERROR"
	case "FATAL", "PANIC", "CRITICAL":
		return "FATAL"
	default:
		return "INFO"
	}
}

func fmtNumStr(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var res []byte
	rem := len(s) % 3
	if rem > 0 {
		res = append(res, s[:rem]...)
		if len(s) > rem {
			res = append(res, ',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		res = append(res, s[i:i+3]...)
		if i+3 < len(s) {
			res = append(res, ',')
		}
	}
	return string(res)
}

func (e *LogEntry) ComputeLevelAndMessage() {
	if e.Level == "" {
		if e.StatusCode == 503 || e.StatusCode == 504 {
			e.Level = "FATAL"
		} else if e.StatusCode >= 500 || e.StatusCode == 403 || e.StatusCode == 401 || e.StatusCode == 400 || e.StatusCode == 404 {
			e.Level = "ERROR"
		} else if e.StatusCode >= 400 {
			e.Level = "WARN"
		} else if e.StatusCode >= 200 {
			e.Level = "INFO"
		} else {
			e.Level = "DEBUG"
		}
	} else {
		e.Level = NormalizeLevel(e.Level)
	}
	if e.Message == "" {
		modeStr := "sync"
		if e.Stream {
			modeStr = "sse"
		}
		pluginTxt := ""
		if e.PluginsApplied != "" {
			if e.TokensSaved > 0 {
				pluginTxt = fmt.Sprintf(" [plugins: %s, saved: %s tok]", e.PluginsApplied, fmtNumStr(e.TokensSaved))
			} else {
				pluginTxt = fmt.Sprintf(" [plugins: %s]", e.PluginsApplied)
			}
		}
		if e.StatusCode >= 400 {
			errTxt := ""
			if e.ErrorMessage != nil && *e.ErrorMessage != "" {
				errTxt = " - " + *e.ErrorMessage
			}
			e.Message = fmt.Sprintf("POST /v1/chat/completions model=%s %d%s%s (%dms, ip=%s)",
				e.Model, e.StatusCode, errTxt, pluginTxt, e.DurationMs, e.ClientIP)
		} else {
			e.Message = fmt.Sprintf("POST /v1/chat/completions model=%s 200 OK%s (%dms, %s tok [p:%s, c:%s], %s, ip=%s)",
				e.Model, pluginTxt, e.DurationMs, fmtNumStr(e.TotalTokens), fmtNumStr(e.PromptTokens), fmtNumStr(e.CompletionTokens), modeStr, e.ClientIP)
		}
	}
}

func parseTimeParam(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1e16 {
			return time.Unix(0, n).UTC(), true
		}
		if n > 1e11 {
			return time.UnixMilli(n).UTC(), true
		}
		return time.Unix(n, 0).UTC(), true
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (m *Manager) Record(entry *LogEntry) error {
	errMsg := sql.NullString{}
	if entry.ErrorMessage != nil && *entry.ErrorMessage != "" {
		errMsg.String = *entry.ErrorMessage
		errMsg.Valid = true
	}

	streamInt := 0
	if entry.Stream {
		streamInt = 1
	}

	// Mask API key: keep first 7 chars and last 4 chars (e.g. sk-proj...1234)
	maskedKey := maskAPIKey(entry.APIKey)
	entry.ComputeLevelAndMessage()

	keyID := sql.NullString{String: entry.APIKeyID, Valid: entry.APIKeyID != ""}
	hasImagesInt := 0
	if entry.HasImages {
		hasImagesInt = 1
	}

	res, err := m.db.Exec(`
		INSERT INTO traffic_logs (
			timestamp, api_key, api_key_name, api_key_id, provider_id, model, prompt_tokens, completion_tokens, total_tokens,
			duration_ms, status_code, client_ip, stream, error_message, level,
			plugins_skipped, plugins_applied, tokens_saved, tokens_overhead, plugin_errors, plugin_ms,
			has_images, image_count
		) VALUES (CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, maskedKey, entry.APIKeyName, keyID, entry.ProviderID, entry.Model, entry.PromptTokens, entry.CompletionTokens, entry.TotalTokens,
		entry.DurationMs, entry.StatusCode, entry.ClientIP, streamInt, errMsg, entry.Level,
		entry.PluginsSkipped, entry.PluginsApplied, entry.TokensSaved, entry.TokensOverhead, entry.PluginErrors, entry.PluginMs,
		hasImagesInt, entry.ImageCount)

	if err == nil {
		if id, idErr := res.LastInsertId(); idErr == nil {
			entry.ID = id
		}
	}

	// Ensure model is recorded in models table
	_, _ = m.db.Exec("INSERT OR IGNORE INTO models (id, name, enabled) VALUES (?, ?, 1)", entry.Model, entry.Model)

	return err
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(strings.ToLower(key), "bearer ") {
		key = strings.TrimSpace(key[7:])
	}
	if len(key) <= 10 {
		if len(key) == 0 {
			return "none"
		}
		return "sk-..." + key[len(key)-2:]
	}
	return key[:6] + "..." + key[len(key)-4:]
}

func (m *Manager) QueryLogs(p FilterParams) ([]LogEntry, int, error) {
	var conditions []string
	var args []interface{}

	if tFrom, ok := parseTimeParam(p.From); ok {
		conditions = append(conditions, "timestamp >= datetime(?)")
		args = append(args, tFrom.Format("2006-01-02 15:04:05"))
	}
	if tTo, ok := parseTimeParam(p.To); ok {
		conditions = append(conditions, "timestamp <= datetime(?)")
		args = append(args, tTo.Format("2006-01-02 15:04:05"))
	}
	if p.From == "" && p.To == "" {
		dateCond, dateArgs := buildDateFilter(p.Period, p.StartDate, p.EndDate, p.Loc)
		if dateCond != "" && dateCond != "1=1" {
			conditions = append(conditions, dateCond)
			args = append(args, dateArgs...)
		}
	}

	if p.Provider != "" {
		conditions = append(conditions, "provider_id = ?")
		args = append(args, p.Provider)
	}

	if p.Model != "" {
		conditions = append(conditions, "model = ?")
		args = append(args, p.Model)
	}

	if p.APIKey != "" {
		conditions = append(conditions, "(api_key = ? OR api_key_name = ? OR api_key LIKE ? OR api_key_name LIKE ?)")
		args = append(args, p.APIKey, p.APIKey, "%"+p.APIKey+"%", "%"+p.APIKey+"%")
	}
	if p.APIKeyID != "" {
		conditions = append(conditions, "api_key_id = ?")
		args = append(args, p.APIKeyID)
	}

	if p.HasImages != nil {
		if *p.HasImages {
			conditions = append(conditions, "has_images = 1")
		} else {
			conditions = append(conditions, "has_images = 0")
		}
	}

	if p.ClientIP != "" {
		conditions = append(conditions, "(client_ip = ? OR client_ip LIKE ?)")
		args = append(args, p.ClientIP, "%"+p.ClientIP+"%")
	}

	if p.Status != "" {
		parts := strings.Split(p.Status, ",")
		has403 := false
		has4xx := false
		var statusConds []string
		for _, raw := range parts {
			s := strings.ToLower(strings.TrimSpace(raw))
			switch s {
			case "2xx", "ok", "success", "200":
				statusConds = append(statusConds, "(status_code >= 200 AND status_code < 300)")
			case "3xx", "redirect":
				statusConds = append(statusConds, "(status_code >= 300 AND status_code < 400)")
			case "403", "blocked":
				has403 = true
			case "4xx", "client", "client_error":
				has4xx = true
			case "5xx", "server", "server_error":
				statusConds = append(statusConds, "(status_code >= 500)")
			case "error", "errors":
				statusConds = append(statusConds, "(status_code >= 400)")
			default:
				if code, err := strconv.Atoi(s); err == nil {
					statusConds = append(statusConds, fmt.Sprintf("(status_code = %d)", code))
				}
			}
		}
		if has403 && has4xx {
			statusConds = append(statusConds, "(status_code >= 400 AND status_code < 500)")
		} else if has403 {
			statusConds = append(statusConds, "(status_code = 403)")
		} else if has4xx {
			statusConds = append(statusConds, "(status_code >= 400 AND status_code < 500 AND status_code != 403)")
		}
		if len(statusConds) > 0 {
			conditions = append(conditions, "("+strings.Join(statusConds, " OR ")+")")
		}
	}

	if p.Level != "" {
		levels := strings.Split(p.Level, ",")
		var levelHolders []string
		var validLevels []string
		for _, l := range levels {
			l = strings.ToUpper(strings.TrimSpace(l))
			if l != "" {
				validLevels = append(validLevels, l)
				levelHolders = append(levelHolders, "?")
			}
		}
		if len(validLevels) > 0 && len(validLevels) < 5 {
			cond := fmt.Sprintf(`(
				CASE
					WHEN level IS NOT NULL AND level != '' THEN UPPER(level)
					WHEN status_code IN (503, 504) THEN 'FATAL'
					WHEN status_code >= 500 OR status_code IN (400, 401, 403, 404) THEN 'ERROR'
					WHEN status_code >= 400 THEN 'WARN'
					WHEN status_code >= 200 THEN 'INFO'
					ELSE 'DEBUG'
				END IN (%s)
			)`, strings.Join(levelHolders, ","))
			conditions = append(conditions, cond)
			for _, vl := range validLevels {
				args = append(args, vl)
			}
		}
	}

	if p.Search != "" {
		terms := strings.Fields(p.Search)
		for _, term := range terms {
			term = strings.TrimSpace(term)
			if term == "" {
				continue
			}
			if colonIdx := strings.Index(term, ":"); colonIdx > 0 {
				prefix := strings.ToLower(term[:colonIdx])
				val := term[colonIdx+1:]
				switch prefix {
				case "model":
					conditions = append(conditions, "model LIKE ?")
					args = append(args, "%"+val+"%")
					continue
				case "key":
					conditions = append(conditions, "(api_key LIKE ? OR api_key_name LIKE ?)")
					args = append(args, "%"+val+"%", "%"+val+"%")
					continue
				case "provider":
					conditions = append(conditions, "provider_id LIKE ?")
					args = append(args, "%"+val+"%")
					continue
				case "ip":
					conditions = append(conditions, "client_ip LIKE ?")
					args = append(args, "%"+val+"%")
					continue
				case "status":
					sVal := strings.ToLower(val)
					if sVal == "blocked" || sVal == "403" {
						conditions = append(conditions, "status_code = 403")
						continue
					} else if sVal == "ok" || sVal == "200" || sVal == "2xx" {
						conditions = append(conditions, "(status_code >= 200 AND status_code < 300)")
						continue
					} else if sVal == "error" || sVal == "errors" {
						conditions = append(conditions, "status_code >= 400")
						continue
					} else if code, err := strconv.Atoi(val); err == nil {
						conditions = append(conditions, "status_code = ?")
						args = append(args, code)
						continue
					}
				}
			}
			likeTerm := "%" + term + "%"
			conditions = append(conditions, `(
				model LIKE ? OR 
				COALESCE(api_key, '') LIKE ? OR 
				COALESCE(api_key_name, '') LIKE ? OR 
				COALESCE(provider_id, '') LIKE ? OR 
				COALESCE(client_ip, '') LIKE ? OR 
				COALESCE(error_message, '') LIKE ?
			)`)
			args = append(args, likeTerm, likeTerm, likeTerm, likeTerm, likeTerm, likeTerm)
		}
	}

	if p.Cursor != "" {
		if cursorID, err := strconv.ParseInt(p.Cursor, 10, 64); err == nil && cursorID > 0 {
			conditions = append(conditions, "id < ?")
			args = append(args, cursorID)
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM traffic_logs " + whereClause
	var total int
	err := m.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Query items
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 500 {
		p.Limit = 500
	}

	query := fmt.Sprintf(`
		SELECT id, timestamp, api_key, COALESCE(api_key_name, ''), COALESCE(api_key_id, ''), COALESCE(provider_id, ''), model, prompt_tokens, completion_tokens, total_tokens,
		       duration_ms, status_code, client_ip, stream, error_message, COALESCE(level, ''),
		       COALESCE(plugins_skipped, ''), COALESCE(plugins_applied, ''), COALESCE(tokens_saved, 0), COALESCE(tokens_overhead, 0),
		       COALESCE(plugin_errors, ''), COALESCE(plugin_ms, 0),
		       COALESCE(has_images, 0), COALESCE(image_count, 0),
		       EXISTS(SELECT 1 FROM models rm WHERE rm.id = traffic_logs.model AND rm.removed_at IS NOT NULL)
		FROM traffic_logs
		%s
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	argsWithLimit := append(args, p.Limit, p.Offset)
	rows, err := m.db.Query(query, argsWithLimit...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []LogEntry
	for rows.Next() {
		var e LogEntry
		var streamInt int
		var errMsg sql.NullString
		var lvlStr string
		var hasImgInt, imgCount int
		var removedInt int
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.APIKey, &e.APIKeyName, &e.APIKeyID, &e.ProviderID, &e.Model,
			&e.PromptTokens, &e.CompletionTokens, &e.TotalTokens,
			&e.DurationMs, &e.StatusCode, &e.ClientIP, &streamInt, &errMsg, &lvlStr,
			&e.PluginsSkipped, &e.PluginsApplied, &e.TokensSaved, &e.TokensOverhead,
			&e.PluginErrors, &e.PluginMs,
			&hasImgInt, &imgCount, &removedInt,
		); err != nil {
			return nil, 0, err
		}
		e.Stream = (streamInt == 1)
		if errMsg.Valid {
			e.ErrorMessage = &errMsg.String
		}
		e.Level = lvlStr
		e.HasImages = (hasImgInt == 1)
		e.ImageCount = imgCount
		e.ModelRemoved = (removedInt == 1)
		e.ComputeLevelAndMessage()
		list = append(list, e)
	}

	return list, total, nil
}

func (m *Manager) GetVolume(p FilterParams, buckets int) (*VolumeResult, error) {
	var from, to time.Time
	var ok bool
	if to, ok = parseTimeParam(p.To); !ok {
		to = time.Now().UTC()
	}
	if from, ok = parseTimeParam(p.From); !ok {
		from, to = periodVolumeWindow(p.Period, p.StartDate, p.EndDate, p.Loc, to)
	}
	if !from.Before(to) {
		from = to.Add(-24 * time.Hour)
	}
	if buckets <= 0 {
		buckets = 60
	}
	if buckets > 720 {
		buckets = 720
	}

	fromUnix := from.Unix()
	toUnix := to.Unix()
	spanSec := toUnix - fromUnix
	if spanSec <= 0 {
		spanSec = 60
	}
	bucketWidthSec := (spanSec + int64(buckets) - 1) / int64(buckets)
	if bucketWidthSec <= 0 {
		bucketWidthSec = 1
	}

	res := &VolumeResult{
		From:        from.UnixNano(),
		To:          to.UnixNano(),
		BucketNanos: bucketWidthSec * 1e9,
		Buckets:     make([]VolumeBucket, buckets),
		Totals: map[string]int64{
			"2xx": 0, "3xx": 0, "403": 0, "4xx": 0, "5xx": 0,
			"DEBUG": 0, "INFO": 0, "WARN": 0, "ERROR": 0, "FATAL": 0,
		},
	}
	for i := range res.Buckets {
		res.Buckets[i] = VolumeBucket{
			Start: (fromUnix + int64(i)*bucketWidthSec) * 1e9,
			Counts: map[string]int64{
				"2xx": 0, "3xx": 0, "403": 0, "4xx": 0, "5xx": 0,
				"DEBUG": 0, "INFO": 0, "WARN": 0, "ERROR": 0, "FATAL": 0,
			},
		}
	}

	var conditions []string
	var args []interface{}

	conditions = append(conditions, "timestamp >= datetime(?)")
	args = append(args, from.Format("2006-01-02 15:04:05"))

	conditions = append(conditions, "timestamp <= datetime(?)")
	args = append(args, to.Format("2006-01-02 15:04:05"))

	if p.Provider != "" {
		conditions = append(conditions, "provider_id = ?")
		args = append(args, p.Provider)
	}
	if p.Model != "" {
		conditions = append(conditions, "model = ?")
		args = append(args, p.Model)
	}
	if p.APIKey != "" {
		conditions = append(conditions, "(api_key = ? OR api_key_name = ? OR api_key LIKE ? OR api_key_name LIKE ?)")
		args = append(args, p.APIKey, p.APIKey, "%"+p.APIKey+"%", "%"+p.APIKey+"%")
	}
	if p.APIKeyID != "" {
		conditions = append(conditions, "api_key_id = ?")
		args = append(args, p.APIKeyID)
	}
	if p.HasImages != nil {
		if *p.HasImages {
			conditions = append(conditions, "has_images = 1")
		} else {
			conditions = append(conditions, "has_images = 0")
		}
	}
	if p.ClientIP != "" {
		conditions = append(conditions, "(client_ip = ? OR client_ip LIKE ?)")
		args = append(args, p.ClientIP, "%"+p.ClientIP+"%")
	}
	if p.Status != "" {
		parts := strings.Split(p.Status, ",")
		has403 := false
		has4xx := false
		var statusConds []string
		for _, raw := range parts {
			s := strings.ToLower(strings.TrimSpace(raw))
			switch s {
			case "2xx", "ok", "success", "200":
				statusConds = append(statusConds, "(status_code >= 200 AND status_code < 300)")
			case "3xx", "redirect":
				statusConds = append(statusConds, "(status_code >= 300 AND status_code < 400)")
			case "403", "blocked":
				has403 = true
			case "4xx", "client", "client_error":
				has4xx = true
			case "5xx", "server", "server_error":
				statusConds = append(statusConds, "(status_code >= 500)")
			case "error", "errors":
				statusConds = append(statusConds, "(status_code >= 400)")
			default:
				if code, err := strconv.Atoi(s); err == nil {
					statusConds = append(statusConds, fmt.Sprintf("(status_code = %d)", code))
				}
			}
		}
		if has403 && has4xx {
			statusConds = append(statusConds, "(status_code >= 400 AND status_code < 500)")
		} else if has403 {
			statusConds = append(statusConds, "(status_code = 403)")
		} else if has4xx {
			statusConds = append(statusConds, "(status_code >= 400 AND status_code < 500 AND status_code != 403)")
		}
		if len(statusConds) > 0 {
			conditions = append(conditions, "("+strings.Join(statusConds, " OR ")+")")
		}
	}
	if p.Level != "" {
		levels := strings.Split(p.Level, ",")
		var levelHolders []string
		var validLevels []string
		for _, l := range levels {
			l = strings.ToUpper(strings.TrimSpace(l))
			if l != "" {
				validLevels = append(validLevels, l)
				levelHolders = append(levelHolders, "?")
			}
		}
		if len(validLevels) > 0 && len(validLevels) < 5 {
			cond := fmt.Sprintf(`(
				CASE
					WHEN level IS NOT NULL AND level != '' THEN UPPER(level)
					WHEN status_code IN (503, 504) THEN 'FATAL'
					WHEN status_code >= 500 OR status_code IN (400, 401, 403, 404) THEN 'ERROR'
					WHEN status_code >= 400 THEN 'WARN'
					WHEN status_code >= 200 THEN 'INFO'
					ELSE 'DEBUG'
				END IN (%s)
			)`, strings.Join(levelHolders, ","))
			conditions = append(conditions, cond)
			for _, vl := range validLevels {
				args = append(args, vl)
			}
		}
	}
	if p.Search != "" {
		terms := strings.Fields(p.Search)
		for _, term := range terms {
			term = strings.TrimSpace(term)
			if term == "" {
				continue
			}
			if colonIdx := strings.Index(term, ":"); colonIdx > 0 {
				prefix := strings.ToLower(term[:colonIdx])
				val := term[colonIdx+1:]
				switch prefix {
				case "model":
					conditions = append(conditions, "model LIKE ?")
					args = append(args, "%"+val+"%")
					continue
				case "key":
					conditions = append(conditions, "(api_key LIKE ? OR api_key_name LIKE ?)")
					args = append(args, "%"+val+"%", "%"+val+"%")
					continue
				case "provider":
					conditions = append(conditions, "provider_id LIKE ?")
					args = append(args, "%"+val+"%")
					continue
				case "ip":
					conditions = append(conditions, "client_ip LIKE ?")
					args = append(args, "%"+val+"%")
					continue
				case "status":
					sVal := strings.ToLower(val)
					if sVal == "blocked" || sVal == "403" {
						conditions = append(conditions, "status_code = 403")
						continue
					} else if sVal == "ok" || sVal == "200" || sVal == "2xx" {
						conditions = append(conditions, "(status_code >= 200 AND status_code < 300)")
						continue
					} else if sVal == "error" || sVal == "errors" {
						conditions = append(conditions, "status_code >= 400")
						continue
					} else if code, err := strconv.Atoi(val); err == nil {
						conditions = append(conditions, "status_code = ?")
						args = append(args, code)
						continue
					}
				}
			}
			likeTerm := "%" + term + "%"
			conditions = append(conditions, `(
				model LIKE ? OR 
				COALESCE(api_key, '') LIKE ? OR 
				COALESCE(api_key_name, '') LIKE ? OR 
				COALESCE(provider_id, '') LIKE ? OR 
				COALESCE(client_ip, '') LIKE ? OR 
				COALESCE(error_message, '') LIKE ?
			)`)
			args = append(args, likeTerm, likeTerm, likeTerm, likeTerm, likeTerm, likeTerm)
		}
	}

	query := fmt.Sprintf(`
		SELECT 
			(CAST(strftime('%%s', timestamp) AS INTEGER) - %d) / %d as b_idx,
			CASE
				WHEN status_code = 403 THEN '403'
				WHEN status_code >= 500 THEN '5xx'
				WHEN status_code >= 400 THEN '4xx'
				WHEN status_code >= 300 THEN '3xx'
				WHEN status_code >= 200 THEN '2xx'
				ELSE 'other'
			END as status_grp,
			COUNT(*) as cnt
		FROM traffic_logs
		WHERE %s
		GROUP BY b_idx, status_grp
	`, fromUnix, bucketWidthSec, strings.Join(conditions, " AND "))

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var bIdx int
		var statusGrp string
		var cnt int64
		if err := rows.Scan(&bIdx, &statusGrp, &cnt); err == nil {
			if bIdx < 0 {
				bIdx = 0
			}
			if bIdx >= buckets {
				bIdx = buckets - 1
			}
			res.Buckets[bIdx].Counts[statusGrp] += cnt
			res.Totals[statusGrp] += cnt

			switch statusGrp {
			case "2xx":
				res.Buckets[bIdx].Counts["INFO"] += cnt
				res.Totals["INFO"] += cnt
			case "403":
				res.Buckets[bIdx].Counts["WARN"] += cnt
				res.Totals["WARN"] += cnt
			case "4xx":
				res.Buckets[bIdx].Counts["ERROR"] += cnt
				res.Totals["ERROR"] += cnt
			case "5xx":
				res.Buckets[bIdx].Counts["ERROR"] += cnt
				res.Totals["ERROR"] += cnt
			default:
				res.Buckets[bIdx].Counts["DEBUG"] += cnt
				res.Totals["DEBUG"] += cnt
			}
		}
	}

	return res, nil
}

func (m *Manager) GetDashboardStats(period, startDate, endDate string, loc *time.Location) (*DashboardStats, error) {
	if loc == nil {
		loc = time.UTC
	}
	dateFilter, dateFilterArgs := buildDateFilter(period, startDate, endDate, loc)
	prevFilter, prevFilterArgs := buildPrevDateFilter(period, startDate, endDate, loc)

	stats := &DashboardStats{}

	// Aggregates
	aggQuery := fmt.Sprintf(`
		SELECT 
			COUNT(*),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0),
			COALESCE(SUM(tokens_saved), 0),
			COALESCE(SUM(CASE WHEN status_code = 403 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 AND status_code != 403 THEN 1 ELSE 0 END), 0),
			COALESCE(ROUND(AVG(duration_ms)), 0),
			COALESCE(MIN(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END), 0),
			COALESCE(MAX(duration_ms), 0),
			COALESCE(SUM(CASE WHEN stream = 1 THEN 1 ELSE 0 END), 0),
			COUNT(DISTINCT model)
		FROM traffic_logs
		WHERE %s
	`, dateFilter)

	err := m.db.QueryRow(aggQuery, dateFilterArgs...).Scan(
		&stats.TotalRequests,
		&stats.TotalTokens,
		&stats.PromptTokens,
		&stats.CompletionTokens,
		&stats.TokensSaved,
		&stats.BlockedRequests,
		&stats.SuccessRequests,
		&stats.ErrorRequests,
		&stats.AvgDurationMs,
		&stats.MinDurationMs,
		&stats.MaxDurationMs,
		&stats.StreamRequests,
		&stats.ActiveModels,
	)
	if err != nil {
		return nil, err
	}

	// Active keys: distinct key groups (key ID, or historical name when unlinked).
	_ = m.db.QueryRow(fmt.Sprintf(`SELECT COUNT(DISTINCT %s) FROM traffic_logs t%s WHERE %s`,
		keyGroupExpr, keyJoin, dateFilter), dateFilterArgs...).Scan(&stats.ActiveKeys)

	stats.SyncRequests = stats.TotalRequests - stats.StreamRequests
	if stats.SyncRequests < 0 {
		stats.SyncRequests = 0
	}

	_ = m.db.QueryRow("SELECT COUNT(*) FROM providers WHERE is_active = 1").Scan(&stats.ActiveProviders)

	if stats.TotalRequests > 0 {
		stats.SuccessRate = float64(stats.SuccessRequests) / float64(stats.TotalRequests) * 100
	} else {
		stats.SuccessRate = 100.0
	}

	// P95 Latency estimation
	p95Query := fmt.Sprintf(`
		SELECT duration_ms FROM traffic_logs
		WHERE %s
		ORDER BY duration_ms ASC
		LIMIT 1 OFFSET ?
	`, dateFilter)
	offsetP95 := int(float64(stats.TotalRequests) * 0.95)
	if offsetP95 > 0 {
		p95Args := append(append([]interface{}{}, dateFilterArgs...), offsetP95)
		_ = m.db.QueryRow(p95Query, p95Args...).Scan(&stats.P95DurationMs)
	} else {
		stats.P95DurationMs = stats.AvgDurationMs
	}

	// Previous period aggregates for comparison
	prevAggQuery := fmt.Sprintf(`
		SELECT 
			COUNT(*),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(CASE WHEN status_code = 403 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0)
		FROM traffic_logs
		WHERE %s
	`, prevFilter)

	var prevReqs, prevToks, prevBlk, prevSucc, prevErrs int
	_ = m.db.QueryRow(prevAggQuery, prevFilterArgs...).Scan(&prevReqs, &prevToks, &prevBlk, &prevSucc, &prevErrs)

	var prevErrRate float64
	if prevReqs > 0 {
		prevErrRate = float64(prevErrs) / float64(prevReqs) * 100.0
	}

	var curErrRate float64
	if stats.TotalRequests > 0 {
		curErrRate = float64(stats.ErrorRequests) / float64(stats.TotalRequests) * 100.0
	}

	var reqDeltaPct float64
	if prevReqs > 0 {
		reqDeltaPct = float64(stats.TotalRequests-prevReqs) / float64(prevReqs) * 100.0
	} else if stats.TotalRequests > 0 {
		reqDeltaPct = 100.0
	}

	var errDeltaPct float64
	if prevErrs > 0 {
		errDeltaPct = float64(stats.ErrorRequests-prevErrs) / float64(prevErrs) * 100.0
	} else if stats.ErrorRequests > 0 {
		errDeltaPct = 100.0
	}

	stats.Comparison = PeriodComparison{
		PrevRequests:     prevReqs,
		PrevErrors:       prevErrs,
		PrevBlocked:      prevBlk,
		PrevTokens:       prevToks,
		PrevSuccess:      prevSucc,
		PrevErrorRate:    prevErrRate,
		ReqDeltaPct:      reqDeltaPct,
		ErrDeltaPct:      errDeltaPct,
		ErrorRateDeltaPt: curErrRate - prevErrRate,
	}

	// Level Mix calculation with comparison
	var prevSuccessShare, prevBlockedShare, prevErrShare float64
	if prevReqs > 0 {
		prevSuccessShare = float64(prevSucc) / float64(prevReqs) * 100.0
		prevBlockedShare = float64(prevBlk) / float64(prevReqs) * 100.0
		prevErrShare = float64(prevErrs-prevBlk) / float64(prevReqs) * 100.0
	}

	curTotal := stats.TotalRequests
	var curSuccessShare, curBlockedShare, curErrShare float64
	otherErrors := stats.ErrorRequests - stats.BlockedRequests
	if otherErrors < 0 {
		otherErrors = 0
	}
	if curTotal > 0 {
		curSuccessShare = float64(stats.SuccessRequests) / float64(curTotal) * 100.0
		curBlockedShare = float64(stats.BlockedRequests) / float64(curTotal) * 100.0
		curErrShare = float64(otherErrors) / float64(curTotal) * 100.0
	}

	stats.LevelMix = []LevelMixStat{
		{
			Level:  "success",
			Label:  "Success (2xx OK)",
			Count:  stats.SuccessRequests,
			Share:  curSuccessShare,
			VsPrev: curSuccessShare - prevSuccessShare,
			Color:  "var(--lv-info)",
		},
		{
			Level:  "blocked",
			Label:  "Policy Blocked (403)",
			Count:  stats.BlockedRequests,
			Share:  curBlockedShare,
			VsPrev: curBlockedShare - prevBlockedShare,
			Color:  "var(--danger)",
		},
		{
			Level:  "error",
			Label:  "Client / Upstream Error",
			Count:  otherErrors,
			Share:  curErrShare,
			VsPrev: curErrShare - prevErrShare,
			Color:  "var(--lv-warn)",
		},
	}

	// Time series setup. Bucket labels are in the viewer's zone: SQLite
	// timestamps (UTC) are shifted by loc's offset at the start of the window.
	var timeFmt string
	var timeSlots []string
	now := nowFunc().In(loc)
	curPeriod, _ := timeutil.ResolvePeriod(period, sanitizeDate(startDate), sanitizeDate(endDate), loc, nowFunc())
	offsetAt := curPeriod.From
	if offsetAt.IsZero() {
		offsetAt = now
	}
	offsetMod := timeutil.SQLiteOffsetModifier(loc, offsetAt)

	if period == "today" || period == "" {
		timeFmt = "%H:00"
		for h := 0; h < 24; h++ {
			timeSlots = append(timeSlots, fmt.Sprintf("%02d:00", h))
		}
	} else if startDate != "" && endDate != "" {
		timeFmt = "%m-%d"
		t1, err1 := time.Parse("2006-01-02", startDate)
		t2, err2 := time.Parse("2006-01-02", endDate)
		if err1 == nil && err2 == nil && !t2.Before(t1) {
			for cur := t1; !cur.After(t2); cur = cur.AddDate(0, 0, 1) {
				timeSlots = append(timeSlots, cur.Format("01-02"))
			}
		} else {
			for d := 13; d >= 0; d-- {
				timeSlots = append(timeSlots, now.AddDate(0, 0, -d).Format("01-02"))
			}
		}
	} else {
		timeFmt = "%m-%d"
		days := 14
		switch period {
		case "7d":
			days = 7
		case "14d":
			days = 14
		case "30d":
			days = 30
		}
		for d := days - 1; d >= 0; d-- {
			timeSlots = append(timeSlots, now.AddDate(0, 0, -d).Format("01-02"))
		}
	}
	// slotExpr is the SQL expression producing a bucket label for a row.
	slotExpr := fmt.Sprintf("strftime('%s', timestamp, '%s')", timeFmt, offsetMod)
	groupFmt := slotExpr

	slotMap := make(map[string]*TimeSeriesPoint)
	for _, slot := range timeSlots {
		slotMap[slot] = &TimeSeriesPoint{Time: slot}
	}

	seriesQuery := fmt.Sprintf(`
		SELECT 
			%s as time_slot,
			COUNT(*) as reqs,
			COALESCE(SUM(total_tokens), 0) as toks,
			COALESCE(SUM(prompt_tokens), 0) as p_toks,
			COALESCE(SUM(completion_tokens), 0) as c_toks,
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0) as errs,
			COALESCE(SUM(CASE WHEN status_code = 403 THEN 1 ELSE 0 END), 0) as blocked,
			COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0) as success,
			COALESCE(ROUND(AVG(duration_ms)), 0) as avg_lat
		FROM traffic_logs
		WHERE %s
		GROUP BY %s
		ORDER BY timestamp ASC
	`, slotExpr, dateFilter, groupFmt)

	rowsSeries, err := m.db.Query(seriesQuery, dateFilterArgs...)
	if err == nil {
		defer rowsSeries.Close()
		for rowsSeries.Next() {
			var slot string
			var reqs, toks, pToks, cToks, errs, blocked, success, avgLat int
			if err := rowsSeries.Scan(&slot, &reqs, &toks, &pToks, &cToks, &errs, &blocked, &success, &avgLat); err == nil {
				var errRate float64
				if reqs > 0 {
					errRate = float64(errs) / float64(reqs) * 100.0
				}
				slotMap[slot] = &TimeSeriesPoint{
					Time:         slot,
					Requests:     reqs,
					Tokens:       toks,
					PromptTokens: pToks,
					CompTokens:   cToks,
					Errors:       errs,
					Blocked:      blocked,
					Success:      success,
					ErrorRate:    errRate,
					AvgLatencyMs: avgLat,
				}
			}
		}
	}

	for _, slot := range timeSlots {
		if pt, ok := slotMap[slot]; ok {
			stats.VolumeSeries = append(stats.VolumeSeries, *pt)
		}
	}

	slotIdx := make(map[string]int)
	for idx, sl := range timeSlots {
		slotIdx[sl] = idx
	}

	// Top models
	topModelsQuery := fmt.Sprintf(`
		SELECT model, COUNT(*) as reqs, COALESCE(SUM(total_tokens), 0) as toks
		FROM traffic_logs
		WHERE %s
		GROUP BY model
		ORDER BY reqs DESC
		LIMIT 5
	`, dateFilter)
	rows, err := m.db.Query(topModelsQuery, dateFilterArgs...)
	if err == nil {
		for rows.Next() {
			var ms ModelStat
			if err := rows.Scan(&ms.Model, &ms.Requests, &ms.Tokens); err == nil {
				if stats.TotalRequests > 0 {
					ms.Share = float64(ms.Requests) / float64(stats.TotalRequests) * 100
				}
				ms.Trend = make([]int, len(timeSlots))
				stats.TopModels = append(stats.TopModels, ms)
			}
		}
		rows.Close()
	}

	modelIdx := make(map[string]int)
	for i, tm := range stats.TopModels {
		modelIdx[tm.Model] = i
	}
	if len(stats.TopModels) > 0 {
		trendQ := fmt.Sprintf(`
			SELECT model, %s as ts, COUNT(*) 
			FROM traffic_logs
			WHERE %s
			GROUP BY model, ts
		`, slotExpr, dateFilter)
		if rTr, errTr := m.db.Query(trendQ, dateFilterArgs...); errTr == nil {
			for rTr.Next() {
				var mod, ts string
				var cnt int
				if err := rTr.Scan(&mod, &ts, &cnt); err == nil {
					if mIdx, ok := modelIdx[mod]; ok {
						if tIdx, ok2 := slotIdx[ts]; ok2 {
							stats.TopModels[mIdx].Trend[tIdx] = cnt
						}
					}
				}
			}
			rTr.Close()
		}
	}

	// Model Mix (for share bar)
	stats.ModelMix = stats.TopModels

	// Top API Keys, grouped by key ID (unlinked rows by historical name).
	tSlotExpr := strings.ReplaceAll(slotExpr, "timestamp", "t.timestamp")
	topKeysQuery := fmt.Sprintf(`
		SELECT %s AS g, %s AS key_name, MAX(t.api_key), COUNT(*) AS reqs, COALESCE(SUM(t.total_tokens), 0) AS toks
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY g
		ORDER BY toks DESC, reqs DESC
		LIMIT 8
	`, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)
	keyIdx := make(map[string]int)
	rowsKeys, err := m.db.Query(topKeysQuery, dateFilterArgs...)
	if err == nil {
		for rowsKeys.Next() {
			var ks KeyStat
			var g string
			var masked sql.NullString
			if err := rowsKeys.Scan(&g, &ks.Name, &masked, &ks.Requests, &ks.Tokens); err == nil {
				ks.KeyID, ks.Unlinked = splitKeyGroup(g)
				ks.Key = masked.String
				if stats.TotalTokens > 0 {
					ks.Share = float64(ks.Tokens) / float64(stats.TotalTokens) * 100
				}
				ks.Trend = make([]int, len(timeSlots))
				keyIdx[g] = len(stats.TopKeys)
				stats.TopKeys = append(stats.TopKeys, ks)
			}
		}
		rowsKeys.Close()
	}

	if len(stats.TopKeys) > 0 {
		trendKeyQ := fmt.Sprintf(`
			SELECT %s AS g, %s AS ts, COUNT(*)
			FROM traffic_logs t%s
			WHERE %s
			GROUP BY g, ts
		`, keyGroupExpr, tSlotExpr, keyJoin, dateFilter)
		if rTr, errTr := m.db.Query(trendKeyQ, dateFilterArgs...); errTr == nil {
			for rTr.Next() {
				var g, ts string
				var cnt int
				if err := rTr.Scan(&g, &ts, &cnt); err == nil {
					if kIdx, ok := keyIdx[g]; ok {
						if tIdx, ok2 := slotIdx[ts]; ok2 {
							stats.TopKeys[kIdx].Trend[tIdx] = cnt
						}
					}
				}
			}
			rTr.Close()
		}
	}

	// Top Error Sources
	topErrQuery := fmt.Sprintf(`
		SELECT 
			COALESCE(NULLIF(model, ''), 'unknown') as source,
			CASE 
				WHEN status_code = 403 THEN 'policy-403'
				WHEN status_code = 401 THEN 'unauthorized-401'
				WHEN status_code >= 500 THEN 'upstream-5xx'
				ELSE 'client-4xx'
			END as namespace,
			COUNT(*) as err_count
		FROM traffic_logs
		WHERE status_code >= 400 AND %s
		GROUP BY source, namespace
		ORDER BY err_count DESC
		LIMIT 5
	`, dateFilter)

	rowsErr, err := m.db.Query(topErrQuery, dateFilterArgs...)
	if err == nil {
		for rowsErr.Next() {
			var es ErrorSourceStat
			if err := rowsErr.Scan(&es.Source, &es.Namespace, &es.Count); err == nil {
				if stats.ErrorRequests > 0 {
					es.Share = float64(es.Count) / float64(stats.ErrorRequests) * 100.0
				}
				es.Trend = make([]int, len(timeSlots))
				stats.TopErrorSources = append(stats.TopErrorSources, es)
			}
		}
		rowsErr.Close()
	}

	errSrcIdx := make(map[string]int)
	for i, es := range stats.TopErrorSources {
		errSrcIdx[es.Source] = i
	}
	if len(stats.TopErrorSources) > 0 {
		trendErrQ := fmt.Sprintf(`
			SELECT model, %s as ts, COUNT(*) 
			FROM traffic_logs
			WHERE status_code >= 400 AND %s
			GROUP BY model, ts
		`, slotExpr, dateFilter)
		if rTrend, errTrend := m.db.Query(trendErrQ, dateFilterArgs...); errTrend == nil {
			for rTrend.Next() {
				var mod, ts string
				var cnt int
				if err := rTrend.Scan(&mod, &ts, &cnt); err == nil {
					if eIdx, ok := errSrcIdx[mod]; ok {
						if tIdx, ok2 := slotIdx[ts]; ok2 {
							stats.TopErrorSources[eIdx].Trend[tIdx] = cnt
						}
					}
				}
			}
			rTrend.Close()
		}
	}

	// Query API Key usage breakdown over date/time slots
	keySeriesQuery := fmt.Sprintf(`
		SELECT
			%s AS time_slot,
			%s AS g,
			%s AS key_name,
			MAX(t.api_key),
			COUNT(*) AS reqs,
			COALESCE(SUM(t.total_tokens), 0) AS toks
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY time_slot, g
		ORDER BY time_slot ASC
	`, tSlotExpr, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)

	rowsKeySeries, err := m.db.Query(keySeriesQuery, dateFilterArgs...)
	if err == nil {
		defer rowsKeySeries.Close()
		for rowsKeySeries.Next() {
			var kp KeyUsageTrendPoint
			var g string
			var masked sql.NullString
			if err := rowsKeySeries.Scan(&kp.Time, &g, &kp.KeyName, &masked, &kp.Requests, &kp.Tokens); err == nil {
				kp.KeyID, _ = splitKeyGroup(g)
				kp.Key = masked.String
				stats.KeyUsageTrends = append(stats.KeyUsageTrends, kp)
			}
		}
	}

	return stats, nil
}

func (m *Manager) GetUsageReports(period, startDate, endDate string, loc *time.Location) (*UsageReport, error) {
	if loc == nil {
		loc = time.UTC
	}
	dateFilter, dateFilterArgs := buildDateFilter(period, startDate, endDate, loc)

	report := &UsageReport{
		Period:          period,
		StartDate:       sanitizeDate(startDate),
		EndDate:         sanitizeDate(endDate),
		KeysBreakdown:   make([]KeyUsageBreakdown, 0),
		ModelsBreakdown: make([]ModelUsageBreakdown, 0),
	}

	// 1. Overall Aggregates
	aggQuery := fmt.Sprintf(`
		SELECT 
			COUNT(*),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0),
			COALESCE(SUM(tokens_saved), 0)
		FROM traffic_logs
		WHERE %s
	`, dateFilter)

	_ = m.db.QueryRow(aggQuery, dateFilterArgs...).Scan(
		&report.TotalRequests,
		&report.TotalTokens,
		&report.PromptTokens,
		&report.CompletionTokens,
		&report.TokensSaved,
	)

	// 2. Query Key-Model Cross Breakdown, grouped by key ID (unlinked rows by historical name).
	keyModelMap := make(map[string][]ModelUsageSummary)
	modelKeyMap := make(map[string][]KeyUsageSummary)

	crossQuery := fmt.Sprintf(`
		SELECT
			%s AS g,
			%s AS key_name,
			MAX(t.api_key),
			t.model,
			COALESCE(SUM(t.total_tokens), 0) AS tot_toks,
			COALESCE(SUM(t.prompt_tokens), 0) AS p_toks,
			COALESCE(SUM(t.completion_tokens), 0) AS c_toks,
			COUNT(*) AS reqs
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY g, t.model
		ORDER BY tot_toks DESC
	`, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)

	crossRows, err := m.db.Query(crossQuery, dateFilterArgs...)
	if err == nil {
		defer crossRows.Close()
		for crossRows.Next() {
			var g, kn, mod string
			var masked sql.NullString
			var tot, pt, ct, reqs int
			if err := crossRows.Scan(&g, &kn, &masked, &mod, &tot, &pt, &ct, &reqs); err == nil {
				keyID, unlinked := splitKeyGroup(g)
				keyModelMap[g] = append(keyModelMap[g], ModelUsageSummary{
					Model:        mod,
					TotalTokens:  tot,
					PromptTokens: pt,
					CompTokens:   ct,
					Requests:     reqs,
				})
				modelKeyMap[mod] = append(modelKeyMap[mod], KeyUsageSummary{
					KeyID:        keyID,
					Unlinked:     unlinked,
					KeyName:      kn,
					Key:          masked.String,
					TotalTokens:  tot,
					PromptTokens: pt,
					CompTokens:   ct,
					Requests:     reqs,
				})
			}
		}
	}

	// 3. Last Active per key and per model: all-time, any status (not period-bound).
	keyLastActive := m.lastActiveBy(keyGroupExpr, keyJoin)
	modelLastActive := m.lastActiveBy("t.model", "")

	// 4. Query Grouping per API Key
	keysQuery := fmt.Sprintf(`
		SELECT
			%s AS g,
			%s AS key_name,
			MAX(t.api_key),
			COALESCE(SUM(t.total_tokens), 0) AS tot_toks,
			COALESCE(SUM(t.prompt_tokens), 0) AS p_toks,
			COALESCE(SUM(t.completion_tokens), 0) AS c_toks,
			COUNT(*) AS tot_reqs,
			COALESCE(SUM(CASE WHEN t.status_code >= 200 AND t.status_code < 400 THEN 1 ELSE 0 END), 0) AS ok_reqs,
			COALESCE(SUM(CASE WHEN t.status_code >= 400 AND t.status_code != 403 THEN 1 ELSE 0 END), 0) AS err_reqs,
			COALESCE(SUM(CASE WHEN t.status_code = 403 THEN 1 ELSE 0 END), 0) AS blk_reqs,
			COALESCE(ROUND(AVG(t.duration_ms)), 0) AS avg_dur,
			COALESCE(SUM(t.tokens_saved), 0) AS tok_saved
		FROM traffic_logs t%s
		WHERE %s
		GROUP BY g
		ORDER BY tot_toks DESC, tot_reqs DESC
	`, keyGroupExpr, keyNameExpr, keyJoin, dateFilter)

	kRows, err := m.db.Query(keysQuery, dateFilterArgs...)
	if err == nil {
		defer kRows.Close()
		for kRows.Next() {
			var kb KeyUsageBreakdown
			var g string
			var masked sql.NullString
			if err := kRows.Scan(
				&g, &kb.KeyName, &masked, &kb.TotalTokens, &kb.PromptTokens, &kb.CompletionTokens,
				&kb.TotalRequests, &kb.SuccessRequests, &kb.ErrorRequests, &kb.BlockedRequests,
				&kb.AvgDurationMs, &kb.TokensSaved,
			); err == nil {
				kb.KeyID, kb.Unlinked = splitKeyGroup(g)
				kb.Key = masked.String
				if report.TotalTokens > 0 {
					kb.TokenShare = float64(kb.TotalTokens) / float64(report.TotalTokens) * 100
				}
				kb.LastActiveAt = keyLastActive[g]
				modelsUsed := keyModelMap[g]
				for i := range modelsUsed {
					if kb.TotalTokens > 0 {
						modelsUsed[i].Share = float64(modelsUsed[i].TotalTokens) / float64(kb.TotalTokens) * 100
					}
				}
				kb.ModelUsage = modelsUsed
				report.KeysBreakdown = append(report.KeysBreakdown, kb)
			}
		}
	}

	// 5. Query Grouping per Model
	modelsQuery := fmt.Sprintf(`
		SELECT 
			t.model,
			COALESCE(m.enabled, 1) as enabled,
			CASE WHEN m.removed_at IS NOT NULL THEN 1 ELSE 0 END as removed,
			COALESCE(SUM(t.total_tokens), 0) as tot_toks,
			COALESCE(SUM(t.prompt_tokens), 0) as p_toks,
			COALESCE(SUM(t.completion_tokens), 0) as c_toks,
			COUNT(t.id) as tot_reqs,
			COALESCE(SUM(CASE WHEN t.status_code >= 200 AND t.status_code < 400 THEN 1 ELSE 0 END), 0) as ok_reqs,
			COALESCE(SUM(CASE WHEN t.status_code >= 400 AND t.status_code != 403 THEN 1 ELSE 0 END), 0) as err_reqs,
			COALESCE(SUM(CASE WHEN t.status_code = 403 THEN 1 ELSE 0 END), 0) as blk_reqs,
			COALESCE(ROUND(AVG(t.duration_ms)), 0) as avg_dur
		FROM traffic_logs t
		LEFT JOIN models m ON t.model = m.id
		WHERE %s
		GROUP BY t.model
		ORDER BY tot_toks DESC, tot_reqs DESC
	`, dateFilter)

	mRows, err := m.db.Query(modelsQuery, dateFilterArgs...)
	if err == nil {
		defer mRows.Close()
		for mRows.Next() {
			var mb ModelUsageBreakdown
			var enInt, rmInt int
			if err := mRows.Scan(
				&mb.Model, &enInt, &rmInt, &mb.TotalTokens, &mb.PromptTokens, &mb.CompletionTokens,
				&mb.TotalRequests, &mb.SuccessRequests, &mb.ErrorRequests, &mb.BlockedRequests,
				&mb.AvgDurationMs,
			); err == nil {
				mb.Enabled = (enInt == 1)
				mb.Removed = (rmInt == 1)
				if report.TotalTokens > 0 {
					mb.TokenShare = float64(mb.TotalTokens) / float64(report.TotalTokens) * 100
				}
				mb.LastActiveAt = modelLastActive[mb.Model]
				consumers := modelKeyMap[mb.Model]
				for i := range consumers {
					if mb.TotalTokens > 0 {
						consumers[i].Share = float64(consumers[i].TotalTokens) / float64(mb.TotalTokens) * 100
					}
				}
				mb.KeyConsumers = consumers
				report.ModelsBreakdown = append(report.ModelsBreakdown, mb)
			}
		}
	}

	if len(report.KeysBreakdown) > 0 {
		report.TopConsumerKey = report.KeysBreakdown[0].KeyName
	}
	if len(report.ModelsBreakdown) > 0 {
		report.TopModel = report.ModelsBreakdown[0].Model
	}

	// 6. Query Breakdown per Plugin
	pluginQuery := fmt.Sprintf(`
		SELECT COALESCE(plugins_applied, ''), COALESCE(SUM(tokens_saved), 0), COUNT(*)
		FROM traffic_logs
		WHERE %s AND plugins_applied != ''
		GROUP BY plugins_applied
	`, dateFilter)
	pRows, err := m.db.Query(pluginQuery, dateFilterArgs...)
	if err == nil {
		defer pRows.Close()
		pMap := make(map[string]*PluginUsageBreakdown)
		for pRows.Next() {
			var applied string
			var saved, reqs int
			if err := pRows.Scan(&applied, &saved, &reqs); err == nil {
				for _, pID := range strings.Split(applied, ",") {
					pID = strings.TrimSpace(pID)
					if pID == "" {
						continue
					}
					if _, exists := pMap[pID]; !exists {
						pMap[pID] = &PluginUsageBreakdown{PluginID: pID}
					}
					pMap[pID].Requests += reqs
					pMap[pID].TokensSaved += saved
				}
			}
		}
		for _, v := range pMap {
			report.PluginsBreakdown = append(report.PluginsBreakdown, *v)
		}
	}

	return report, nil
}

// lastActiveBy returns MAX(timestamp) over all traffic (no period filter, all
// status codes) grouped by groupExpr, normalized to RFC 3339 UTC. traffic_logs
// is aliased t and join is appended after it. Both arguments must be trusted
// constants, never user input.
func (m *Manager) lastActiveBy(groupExpr, join string) map[string]*string {
	out := make(map[string]*string)
	rows, err := m.db.Query(fmt.Sprintf(
		"SELECT %s AS g, MAX(t.timestamp) FROM traffic_logs t%s GROUP BY g", groupExpr, join))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g sql.NullString
		var ts sql.NullString
		if err := rows.Scan(&g, &ts); err == nil && g.Valid {
			out[g.String] = timeutil.NullTimeString(ts)
		}
	}
	return out
}

// periodVolumeWindow converts a period into the [from, to] span used by volume
// histograms, with calendar boundaries in loc. "today" spans the whole local
// day so the chart has a stable width; open-ended windows end at to.
func periodVolumeWindow(period, startDate, endDate string, loc *time.Location, to time.Time) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	cur, _ := timeutil.ResolvePeriod(period, startDate, endDate, loc, nowFunc())
	from := cur.From
	if !cur.To.IsZero() {
		to = cur.To.Add(-time.Second)
	} else if !from.IsZero() && (period == "" || period == "today") && startDate == "" && endDate == "" {
		to = from.In(loc).AddDate(0, 0, 1).UTC().Add(-time.Second)
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	return from, to
}
