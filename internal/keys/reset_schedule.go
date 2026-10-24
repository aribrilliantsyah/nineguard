package keys

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"nineguard/internal/traffic"
)

// Where an API Key's Effective Reset Schedule comes from (ADR 0010 #2).
const (
	ScheduleSourceKey     = "key"
	ScheduleSourceGlobal  = "global"
	ScheduleSourceDefault = "default"
)

// settingGlobalSchedule is the settings row holding the Global Reset Schedule (JSON).
const settingGlobalSchedule = "quota_reset_schedule"

// ScheduleChange says what a key update does to the key's own schedule. A nil
// *ScheduleChange leaves it untouched; Clear removes it (the key follows the global
// schedule again); otherwise Value replaces it.
type ScheduleChange struct {
	Clear bool
	Value *traffic.ResetSchedule
}

// ReloadGlobalSchedule re-reads the Global Reset Schedule from settings. An absent or
// unusable value means "unset".
func (m *Manager) ReloadGlobalSchedule() {
	var raw string
	if err := m.db.QueryRow(`SELECT value FROM settings WHERE key = ? LIMIT 1`, settingGlobalSchedule).Scan(&raw); err != nil {
		raw = ""
	}
	s := parseSchedule(raw, "global")
	m.mu.Lock()
	m.globalSched = s
	m.mu.Unlock()
}

// globalSchedule is the cached Global Reset Schedule (nil = unset).
func (m *Manager) globalSchedule() *traffic.ResetSchedule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.globalSched
}

// parseSchedule reads a stored schedule. Anything unusable is treated as absent with a
// warning: a bad row must never block traffic or hide a key (ADR 0010).
func parseSchedule(raw, owner string) *traffic.ResetSchedule {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var s traffic.ResetSchedule
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		slog.Warn("ignoring unreadable quota reset schedule", "owner", owner, "error", err)
		return nil
	}
	if err := s.Validate(); err != nil {
		slog.Warn("ignoring invalid quota reset schedule", "owner", owner, "error", err)
		return nil
	}
	return &s
}

// parsePins reads stored Transitional Windows, skipping malformed entries.
func parsePins(raw, owner string) map[string]traffic.WindowPin {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var pins map[string]traffic.WindowPin
	if err := json.Unmarshal([]byte(raw), &pins); err != nil {
		slog.Warn("ignoring unreadable quota window pins", "owner", owner, "error", err)
		return nil
	}
	for period, p := range pins {
		if !p.Until.After(p.Start) {
			delete(pins, period)
		}
	}
	if len(pins) == 0 {
		return nil
	}
	return pins
}

// effectiveSchedule resolves key schedule, else global, else the ADR 0005 default (nil).
func effectiveSchedule(own, global *traffic.ResetSchedule) (*traffic.ResetSchedule, string) {
	switch {
	case own != nil:
		return own, ScheduleSourceKey
	case global != nil:
		return global, ScheduleSourceGlobal
	default:
		return nil, ScheduleSourceDefault
	}
}

// applyResetSchedule fills the schedule fields of ki from its stored columns. Every path that
// loads an api_keys row calls it, so they cannot disagree.
func applyResetSchedule(ki *KeyInfo, ownRaw, pinsRaw string, global *traffic.ResetSchedule) {
	ki.ResetSchedule = parseSchedule(ownRaw, ki.ID)
	ki.EffectiveResetSchedule, ki.ResetScheduleSource = effectiveSchedule(ki.ResetSchedule, global)
	ki.ResetRule = traffic.ResetRule{Schedule: ki.EffectiveResetSchedule, Pins: parsePins(pinsRaw, ki.ID)}
}

// encodeSchedule serializes a schedule for the column; nil binds as NULL.
func encodeSchedule(s *traffic.ResetSchedule) any {
	if s == nil {
		return nil
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// encodePins serializes Transitional Windows for the column; none binds as NULL.
func encodePins(pins map[string]traffic.WindowPin) any {
	if len(pins) == 0 {
		return nil
	}
	b, _ := json.Marshal(pins)
	return string(b)
}

// pinsAfterChange is the one place that decides which pins a key needs when its Effective
// Reset Schedule goes from oldEff to newEff. An unchanged schedule keeps the stored pins as
// they are (saving a form that re-sends the same schedule must not rewrite them).
func pinsAfterChange(oldEff, newEff *traffic.ResetSchedule, oldPins map[string]traffic.WindowPin, now time.Time) map[string]traffic.WindowPin {
	if oldEff.Equal(newEff) {
		return oldPins
	}
	return traffic.PinsForChange(traffic.ResetRule{Schedule: oldEff, Pins: oldPins}, newEff, now)
}
