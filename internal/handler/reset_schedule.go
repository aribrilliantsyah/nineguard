package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"nineguard/internal/auth"
	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

// scheduleField is the wire form of a Quota Reset Schedule. Pointers tell a missing field from
// a zero value, so an incomplete object is a 400 instead of silently meaning "weekday 0".
type scheduleField struct {
	Timezone *string `json:"timezone"`
	Time     *string `json:"time"`
	Weekday  *int    `json:"weekday"`
	Day      *int    `json:"day"`
}

// optSchedule is a quota_reset_schedule property that may be absent, null, or an object.
type optSchedule struct {
	Present bool
	Null    bool
	Value   *traffic.ResetSchedule
}

// parseOptSchedule reads the raw JSON value of quota_reset_schedule. A nil raw means the
// property was absent. The error text names the offending field.
func parseOptSchedule(raw json.RawMessage) (optSchedule, error) {
	if raw == nil {
		return optSchedule{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return optSchedule{Present: true, Null: true}, nil
	}
	var f scheduleField
	if err := json.Unmarshal(raw, &f); err != nil {
		return optSchedule{}, fmt.Errorf("quota_reset_schedule must be an object with timezone, time, weekday and day")
	}
	switch {
	case f.Timezone == nil:
		return optSchedule{}, fmt.Errorf("quota_reset_schedule.timezone is required")
	case f.Time == nil:
		return optSchedule{}, fmt.Errorf("quota_reset_schedule.time is required")
	case f.Weekday == nil:
		return optSchedule{}, fmt.Errorf("quota_reset_schedule.weekday is required")
	case f.Day == nil:
		return optSchedule{}, fmt.Errorf("quota_reset_schedule.day is required")
	}
	s := &traffic.ResetSchedule{Timezone: *f.Timezone, Time: *f.Time, Weekday: *f.Weekday, Day: *f.Day}
	if err := s.Validate(); err != nil {
		return optSchedule{}, err
	}
	return optSchedule{Present: true, Value: s}, nil
}

// scheduleChange turns the parsed property into a keys.ScheduleChange for updates.
func (o optSchedule) scheduleChange() *keys.ScheduleChange {
	switch {
	case !o.Present:
		return nil
	case o.Null:
		return &keys.ScheduleChange{Clear: true}
	default:
		return &keys.ScheduleChange{Value: o.Value}
	}
}

// scheduleResponse is the global schedule plus how many keys follow it or hold their own.
type scheduleResponse struct {
	Schedule *traffic.ResetSchedule `json:"quota_reset_schedule"`
	keys.ScheduleCounts
}

func (h *Handler) scheduleSnapshot(w http.ResponseWriter, counts keys.ScheduleCounts, err error) {
	if err != nil {
		slog.Error("failed to read reset schedule counts", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to read the reset schedule")
		return
	}
	jsonResponse(w, http.StatusOK, scheduleResponse{Schedule: h.keys.GetGlobalSchedule(), ScheduleCounts: counts})
}

func (h *Handler) actor(r *http.Request) (int64, string) {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.ID, u.Username
	}
	return 0, ""
}

// GetGlobalResetSchedule returns the Global Reset Schedule and the follower/custom key counts.
func (h *Handler) GetGlobalResetSchedule(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	counts, err := h.keys.ScheduleCounts()
	h.scheduleSnapshot(w, counts, err)
}

// SetGlobalResetSchedule sets the Global Reset Schedule, or removes it with null (ADR 0010).
func (h *Handler) SetGlobalResetSchedule(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	var body struct {
		Schedule json.RawMessage `json:"quota_reset_schedule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	opt, err := parseOptSchedule(body.Schedule)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !opt.Present {
		jsonError(w, http.StatusBadRequest, "quota_reset_schedule is required (an object, or null to remove it)")
		return
	}
	counts, err := h.keys.SetGlobalSchedule(opt.Value) // nil removes it
	if err != nil {
		slog.Error("failed to set global reset schedule", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to save the reset schedule")
		return
	}
	id, name := h.actor(r)
	h.logAudit(r, id, name, "quota.reset_schedule.set", "global", "success", "")
	h.scheduleSnapshot(w, counts, nil)
}

// ClearResetScheduleOverrides makes every key follow the Global Reset Schedule.
func (h *Handler) ClearResetScheduleOverrides(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	counts, err := h.keys.ClearScheduleOverrides()
	if err != nil {
		slog.Error("failed to clear reset schedule overrides", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to clear the overrides")
		return
	}
	id, name := h.actor(r)
	h.logAudit(r, id, name, "quota.reset_schedule.clear_overrides", "global", "success", "")
	h.scheduleSnapshot(w, counts, nil)
}

// PreviewQuotaReset computes the next resets of a proposed schedule without saving it.
func (h *Handler) PreviewQuotaReset(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	var body struct {
		KeyID    string          `json:"key_id"`
		Schedule json.RawMessage `json:"quota_reset_schedule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid payload")
		return
	}
	opt, err := parseOptSchedule(body.Schedule)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := h.keys.PreviewReset(opt.Value, body.KeyID, time.Now())
	if err != nil {
		if err.Error() == "api key not found" {
			jsonError(w, http.StatusNotFound, "API key not found")
			return
		}
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, p)
}
