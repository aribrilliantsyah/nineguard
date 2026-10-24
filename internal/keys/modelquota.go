package keys

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"nineguard/internal/models"
	"nineguard/internal/traffic"
)

const (
	QuotaModeKey   = "key"
	QuotaModeModel = "model"
)

// ModelQuota is one per-Model limit on an API Key (ADR 0007). The same type carries the
// read-only computed fields (usage, percent, status, reset, inactive state) in responses;
// they are ignored on input.
type ModelQuota struct {
	ModelID        string  `json:"model_id"`
	QuotaLimit     int64   `json:"quota_limit"`
	QuotaPeriod    string  `json:"quota_period"`
	Usage          int64   `json:"usage"`
	Percent        int     `json:"percent"`
	Status         string  `json:"status"`
	ResetAt        *string `json:"reset_at"`
	Inactive       bool    `json:"inactive"`
	InactiveReason string  `json:"inactive_reason,omitempty"`

	// Set only on the Key Quota object of the quotas overview (ADR 0010): the key's own schedule
	// (null = follows the global one), the one in effect, and where it comes from.
	ResetSchedule          *traffic.ResetSchedule `json:"quota_reset_schedule,omitempty"`
	EffectiveResetSchedule *traffic.ResetSchedule `json:"effective_quota_reset_schedule,omitempty"`
	ResetScheduleSource    string                 `json:"quota_reset_schedule_source,omitempty"`
}

// ModelQuotaSummary is the compact form used by key list responses.
type ModelQuotaSummary struct {
	Count      int    `json:"count"`
	Active     int    `json:"active"`
	TopModelID string `json:"top_model_id"`
	TopPercent int    `json:"top_percent"`
}

// NormalizeQuotaMode maps "" to "key" and reports whether the mode is valid.
func NormalizeQuotaMode(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", QuotaModeKey:
		return QuotaModeKey, true
	case QuotaModeModel:
		return QuotaModeModel, true
	}
	return "", false
}

// IsModelQuotaPeriod: Model Quotas have no "none" period (no row means unlimited).
func IsModelQuotaPeriod(p string) bool {
	switch p {
	case "daily", "weekly", "monthly", "total":
		return true
	}
	return false
}

// QuotaPercent is floor(usage*100/limit), deliberately not capped at 100.
func QuotaPercent(usage, limit int64) int {
	if limit <= 0 || usage <= 0 {
		return 0
	}
	return int(usage * 100 / limit)
}

// QuotaStatus is "exhausted" once usage reaches the limit (same test the proxy uses),
// "near" at or above the warn percent, otherwise "ok".
func QuotaStatus(usage, limit int64, warn int) string {
	if limit > 0 && usage >= limit {
		return "exhausted"
	}
	if QuotaPercent(usage, limit) >= warn {
		return "near"
	}
	return "ok"
}

func (ki *KeyInfo) EnforcesModelQuotas() bool {
	return ki != nil && ki.QuotaMode == QuotaModeModel
}

// ModelQuotaFor finds the row for a resolved Model ID (case-insensitive).
func (ki *KeyInfo) ModelQuotaFor(modelID string) *ModelQuota {
	if ki == nil {
		return nil
	}
	for i := range ki.ModelQuotas {
		if strings.EqualFold(ki.ModelQuotas[i].ModelID, modelID) {
			return &ki.ModelQuotas[i]
		}
	}
	return nil
}

type quotaQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// loadModelQuotaRows reads raw rows (keyID == "" means all keys) into a map keyed by API key id.
// The rows are closed before it returns: the database has a single connection.
func loadModelQuotaRows(q quotaQuerier, keyID string) (map[string][]ModelQuota, error) {
	query := `SELECT api_key_id, model_id, quota_limit, quota_period FROM api_key_model_quotas`
	var args []any
	if keyID != "" {
		query += ` WHERE api_key_id = ?`
		args = append(args, keyID)
	}
	query += ` ORDER BY api_key_id, model_id COLLATE NOCASE`
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]ModelQuota{}
	for rows.Next() {
		var kid string
		var mq ModelQuota
		if err := rows.Scan(&kid, &mq.ModelID, &mq.QuotaLimit, &mq.QuotaPeriod); err != nil {
			return nil, err
		}
		out[kid] = append(out[kid], mq)
	}
	return out, rows.Err()
}

// GetModelQuotas returns the stored rows of one key (limit and period only).
func (m *Manager) GetModelQuotas(keyID string) ([]ModelQuota, error) {
	mp, err := loadModelQuotaRows(m.db, keyID)
	return mp[keyID], err
}

// DeriveAccessMode applies the same default as key creation/update: an explicit mode wins,
// otherwise groups mean "group", an allow-list means "custom", nothing means "all".
func DeriveAccessMode(mode string, groupIDs, allowed []string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "" {
		return mode
	}
	if len(groupIDs) > 0 {
		return "group"
	}
	if len(allowed) > 0 {
		return "custom"
	}
	return "all"
}

// AccessPreview returns a KeyInfo bound to this manager so IsModelAllowed can resolve Model
// Groups for access settings that are not saved yet (used to validate new Model Quota rows).
func (m *Manager) AccessPreview(mode string, groupIDs, allowed []string) *KeyInfo {
	return &KeyInfo{
		ModelAccessMode: DeriveAccessMode(mode, groupIDs, allowed),
		ModelGroupIDs:   groupIDs,
		AllowedModels:   allowed,
		manager:         m,
	}
}

// replaceModelQuotas rewrites all rows of one key inside tx.
func replaceModelQuotas(tx *sql.Tx, keyID string, rows []ModelQuota) error {
	if _, err := tx.Exec(`DELETE FROM api_key_model_quotas WHERE api_key_id = ?`, keyID); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO api_key_model_quotas (api_key_id, model_id, quota_limit, quota_period) VALUES (?,?,?,?)`,
			keyID, r.ModelID, r.QuotaLimit, r.QuotaPeriod); err != nil {
			return fmt.Errorf("model quota %q: %w", r.ModelID, err)
		}
	}
	return nil
}

// KeyUpdate is everything the key modal saves at once. Nil pointers mean "leave unchanged";
// a non-nil ModelQuotas (even empty) replaces the key's whole set.
type KeyUpdate struct {
	Name            string
	ModelAccessMode string
	ModelGroupIDs   []string
	AllowedModels   []string
	QuotaLimit      *int64
	QuotaPeriod     *string
	QuotaMode       *string
	ModelQuotas     *[]ModelQuota
	ResetSchedule   *ScheduleChange // nil leaves the key's own schedule and pins alone (ADR 0010)
	CostVisibility  *string         // nil leaves unchanged; non-nil with "show", "hide", or "" (resets to null)
}

// UpdateKeyAll saves the key, its Key Quota, mode and Model Quotas in one transaction.
func (m *Manager) UpdateKeyAll(id string, u KeyUpdate) (*KeyInfo, error) {
	name := strings.TrimSpace(u.Name)
	if name == "" {
		return nil, fmt.Errorf("key name cannot be empty")
	}
	mode := DeriveAccessMode(u.ModelAccessMode, u.ModelGroupIDs, u.AllowedModels)

	var qLimit, qPeriod, qMode any // nil binds as NULL, so COALESCE keeps the stored value
	if u.QuotaLimit != nil {
		v := *u.QuotaLimit
		if v < 0 {
			v = 0
		}
		qLimit = v
	}
	if u.QuotaPeriod != nil {
		p := strings.ToLower(strings.TrimSpace(*u.QuotaPeriod))
		if p == "" {
			p = "none"
		}
		qPeriod = p
	}
	if u.QuotaMode != nil {
		qm, ok := NormalizeQuotaMode(*u.QuotaMode)
		if !ok {
			return nil, fmt.Errorf("invalid quota_mode")
		}
		qMode = qm
	}

	tx, err := m.db.Begin() // from here on use ONLY tx: the database has a single connection
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// A schedule change keeps usage: the window running now keeps its start (ADR 0010 #5), so
	// the pins are computed against the stored schedule and written with the new one.
	if u.ResetSchedule != nil {
		var ownRaw, pinsRaw string
		if err := tx.QueryRow(`SELECT COALESCE(quota_reset_schedule,''), COALESCE(quota_window_pins,'') FROM api_keys WHERE id = ?`, id).
			Scan(&ownRaw, &pinsRaw); err != nil {
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("api key not found")
			}
			return nil, err
		}
		global := m.globalSchedule()
		oldOwn := parseSchedule(ownRaw, id)
		oldEff, _ := effectiveSchedule(oldOwn, global)
		newOwn := oldOwn
		switch {
		case u.ResetSchedule.Clear:
			newOwn = nil
		case u.ResetSchedule.Value != nil:
			newOwn = u.ResetSchedule.Value
		}
		newEff, _ := effectiveSchedule(newOwn, global)
		pins := pinsAfterChange(oldEff, newEff, parsePins(pinsRaw, id), time.Now())
		if _, err := tx.Exec(`UPDATE api_keys SET quota_reset_schedule = ?, quota_window_pins = ? WHERE id = ?`,
			encodeSchedule(newOwn), encodePins(pins), id); err != nil {
			return nil, err
		}
	}

	res, err := tx.Exec(`UPDATE api_keys SET name = ?, model_access_mode = ?, model_group_ids = ?, allowed_models = ?,
		quota_limit = COALESCE(?, quota_limit), quota_period = COALESCE(?, quota_period), quota_mode = COALESCE(?, quota_mode),
		updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, mode, SerializeAllowedModels(u.ModelGroupIDs), SerializeAllowedModels(u.AllowedModels), qLimit, qPeriod, qMode, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("api key not found")
	}
	if u.ModelQuotas != nil {
		if err := replaceModelQuotas(tx, id, *u.ModelQuotas); err != nil {
			return nil, err
		}
	}
	if u.CostVisibility != nil {
		cv := strings.ToLower(strings.TrimSpace(*u.CostVisibility))
		var val *string
		if cv == "show" || cv == "hide" {
			val = &cv
		}
		if _, err := tx.Exec(`UPDATE api_keys SET cost_visibility = ? WHERE id = ?`, val, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	m.loadLocalCache()
	return m.GetKey(id)
}

// inactiveReason says why a stored Model Quota cannot currently apply, or "" when it can.
// Order is removed, disabled, no_access: the effective allow-list of group/custom keys already
// drops Removed Models, so checking access first would mislabel them. Read-only lookups only
// (models.IsModelEnabled would auto-register unknown ids).
func (m *Manager) inactiveReason(ki *KeyInfo, modelID string) string {
	found, enabled, removed := models.LookupStatus(m.db, modelID)
	if !found || removed {
		return "removed"
	}
	if !enabled {
		return "disabled"
	}
	if !ki.IsModelAllowed(modelID) {
		return "no_access"
	}
	return ""
}

func resetAtString(period string, resetAt time.Time) *string {
	if period == "total" {
		return nil
	}
	s := resetAt.UTC().Format(time.RFC3339)
	return &s
}

// AnnotateModelQuotas fills usage, percent, status, reset and inactive state on ki.ModelQuotas
// in place. It runs one grouped usage query per distinct period of the key.
func (m *Manager) AnnotateModelQuotas(ki *KeyInfo, warn int, now time.Time) {
	usageByPeriod := map[string]map[string]int64{}
	for i := range ki.ModelQuotas {
		mq := &ki.ModelQuotas[i]
		usage, ok := usageByPeriod[mq.QuotaPeriod]
		if !ok {
			start, _ := traffic.CalcQuotaWindow(mq.QuotaPeriod, ki.ResetRule, now)
			usage, _ = traffic.GetModelUsageSince(m.db, ki.ID, start)
			if usage == nil {
				usage = map[string]int64{}
			}
			usageByPeriod[mq.QuotaPeriod] = usage
		}
		_, resetAt := traffic.CalcQuotaWindow(mq.QuotaPeriod, ki.ResetRule, now)
		mq.Usage = usage[strings.ToLower(mq.ModelID)]
		mq.Percent = QuotaPercent(mq.Usage, mq.QuotaLimit)
		mq.ResetAt = resetAtString(mq.QuotaPeriod, resetAt)
		mq.InactiveReason = m.inactiveReason(ki, mq.ModelID)
		mq.Inactive = mq.InactiveReason != ""
		if mq.Inactive {
			mq.Status = "inactive"
		} else {
			mq.Status = QuotaStatus(mq.Usage, mq.QuotaLimit, warn)
		}
	}
}

// SummarizeModelQuotas is the compact list form. Only active rows count towards the top row.
func SummarizeModelQuotas(rows []ModelQuota) *ModelQuotaSummary {
	if len(rows) == 0 {
		return nil
	}
	s := &ModelQuotaSummary{Count: len(rows)}
	for _, r := range rows {
		if r.Inactive {
			continue
		}
		s.Active++
		if s.TopModelID == "" || r.Percent > s.TopPercent {
			s.TopModelID, s.TopPercent = r.ModelID, r.Percent
		}
	}
	return s
}
