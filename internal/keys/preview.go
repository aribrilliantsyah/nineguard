package keys

import (
	"database/sql"
	"fmt"
	"time"

	"nineguard/internal/traffic"
)

// ResetPreview is the next reset of each scheduled Quota Period under a proposed schedule, as
// the server would compute it after saving. Transitional lists the periods whose current window
// would be cut or stretched into a Transitional Window (usage is kept either way).
type ResetPreview struct {
	Daily        string   `json:"daily"`
	Weekly       string   `json:"weekly"`
	Monthly      string   `json:"monthly"`
	Transitional []string `json:"transitional"`
}

// PreviewReset computes ResetPreview for schedule s (nil = no own schedule). With keyID the
// key's current rule and the pins its change would write are applied, and nil means "follow the
// global schedule"; without keyID it is the pure schedule (nil = the UTC default). It reads only.
func (m *Manager) PreviewReset(s *traffic.ResetSchedule, keyID string, now time.Time) (*ResetPreview, error) {
	if s != nil {
		if err := s.Validate(); err != nil {
			return nil, err
		}
	}

	rule := traffic.ResetRule{Schedule: s}
	transitional := []string{}

	if keyID != "" {
		var ownRaw, pinsRaw string
		err := m.db.QueryRow(`SELECT COALESCE(quota_reset_schedule,''), COALESCE(quota_window_pins,'') FROM api_keys WHERE id = ?`, keyID).
			Scan(&ownRaw, &pinsRaw)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("api key not found")
		}
		if err != nil {
			return nil, err
		}
		global := m.globalSchedule()
		oldOwn := parseSchedule(ownRaw, keyID)
		oldEff, _ := effectiveSchedule(oldOwn, global)
		newEff, _ := effectiveSchedule(s, global)
		oldPins := parsePins(pinsRaw, keyID)

		pins := pinsAfterChange(oldEff, newEff, oldPins, now)
		rule = traffic.ResetRule{Schedule: newEff, Pins: pins}
		// a period is transitional only when this change creates or alters its pin
		for _, period := range []string{"daily", "weekly", "monthly"} {
			p, ok := pins[period]
			if !ok {
				continue
			}
			if o, had := oldPins[period]; had && o == p {
				continue
			}
			transitional = append(transitional, period)
		}
	}

	next := func(period string) string {
		_, reset := traffic.CalcQuotaWindow(period, rule, now)
		return reset.UTC().Format(time.RFC3339)
	}
	return &ResetPreview{
		Daily:        next("daily"),
		Weekly:       next("weekly"),
		Monthly:      next("monthly"),
		Transitional: transitional,
	}, nil
}
