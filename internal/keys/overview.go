package keys

import (
	"sort"
	"strings"
	"time"

	"nineguard/internal/traffic"
)

type OverviewSummary struct {
	KeysWithQuota int `json:"keys_with_quota"`
	ModelQuotas   int `json:"model_quotas"`
	NearLimit     int `json:"near_limit"`
	Exhausted     int `json:"exhausted"`
}

// OverviewKey is one API Key on the Quotas page. KeyQuota is set for key-mode keys that
// enforce one; ModelQuotas only for model-mode keys. Status: ok, near, exhausted or unlimited.
type OverviewKey struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	IsActive    bool         `json:"is_active"`
	QuotaMode   string       `json:"quota_mode"`
	TopPercent  int          `json:"top_percent"`
	Status      string       `json:"status"`
	KeyQuota    *ModelQuota  `json:"key_quota"`
	ModelQuotas []ModelQuota `json:"model_quotas"`

	// The schedule in effect for this key (ADR 0010), also for model-mode keys that have no
	// key_quota object. A nil effective schedule is the UTC default.
	EffectiveResetSchedule *traffic.ResetSchedule `json:"effective_quota_reset_schedule"`
	ResetScheduleSource    string                 `json:"quota_reset_schedule_source"`
}

type QuotaOverview struct {
	WarnPercent int             `json:"warn_percent"`
	Summary     OverviewSummary `json:"summary"`
	Keys        []OverviewKey   `json:"keys"`
}

func worseStatus(a, b string) string {
	rank := map[string]int{"unlimited": 0, "ok": 1, "near": 2, "exhausted": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// QuotaOverview builds the data for the Quotas page and dashboard card. Keys come from the
// in-memory cache (copied before any query runs); usage is read per key and window.
func (m *Manager) QuotaOverview(warn int, now time.Time) (*QuotaOverview, error) {
	m.mu.RLock()
	snap := make([]KeyInfo, 0, len(m.cache))
	for _, ki := range m.cache {
		c := *ki
		c.ModelQuotas = append([]ModelQuota(nil), ki.ModelQuotas...)
		snap = append(snap, c)
	}
	m.mu.RUnlock()

	out := &QuotaOverview{WarnPercent: warn, Keys: make([]OverviewKey, 0, len(snap))}
	for i := range snap {
		ki := &snap[i]
		ok := OverviewKey{ID: ki.ID, Name: ki.Name, IsActive: ki.IsActive, QuotaMode: ki.QuotaMode, Status: "unlimited", ModelQuotas: []ModelQuota{},
			EffectiveResetSchedule: ki.EffectiveResetSchedule, ResetScheduleSource: ki.ResetScheduleSource}

		if ki.QuotaMode == QuotaModeModel {
			m.AnnotateModelQuotas(ki, warn, now)
			ok.ModelQuotas = ki.ModelQuotas
			out.Summary.ModelQuotas += len(ki.ModelQuotas)
			for _, r := range ki.ModelQuotas {
				if r.Inactive {
					continue
				}
				ok.Status = worseStatus(ok.Status, r.Status)
				if r.Percent > ok.TopPercent {
					ok.TopPercent = r.Percent
				}
				switch r.Status {
				case "near":
					out.Summary.NearLimit++
				case "exhausted":
					out.Summary.Exhausted++
				}
			}
			sort.SliceStable(ok.ModelQuotas, func(a, b int) bool {
				x, y := ok.ModelQuotas[a], ok.ModelQuotas[b]
				if x.Inactive != y.Inactive {
					return !x.Inactive
				}
				if x.Percent != y.Percent {
					return x.Percent > y.Percent
				}
				return strings.ToLower(x.ModelID) < strings.ToLower(y.ModelID)
			})
		} else if ki.QuotaLimit > 0 && ki.QuotaPeriod != "" && ki.QuotaPeriod != "none" {
			usage, resetAt, _ := traffic.GetQuotaUsage(m.db, ki.ID, ki.QuotaPeriod, ki.ResetRule, now)
			kq := ModelQuota{
				QuotaLimit: ki.QuotaLimit, QuotaPeriod: ki.QuotaPeriod, Usage: usage,
				Percent: QuotaPercent(usage, ki.QuotaLimit), Status: QuotaStatus(usage, ki.QuotaLimit, warn),
				ResetAt: resetAtString(ki.QuotaPeriod, resetAt),

				ResetSchedule: ki.ResetSchedule, EffectiveResetSchedule: ki.EffectiveResetSchedule, ResetScheduleSource: ki.ResetScheduleSource,
			}
			ok.KeyQuota, ok.Status, ok.TopPercent = &kq, kq.Status, kq.Percent
			switch kq.Status {
			case "near":
				out.Summary.NearLimit++
			case "exhausted":
				out.Summary.Exhausted++
			}
		}
		if ok.Status != "unlimited" {
			out.Summary.KeysWithQuota++
		}
		out.Keys = append(out.Keys, ok)
	}
	sort.SliceStable(out.Keys, func(a, b int) bool {
		x, y := out.Keys[a], out.Keys[b]
		if x.TopPercent != y.TopPercent {
			return x.TopPercent > y.TopPercent
		}
		return strings.ToLower(x.Name) < strings.ToLower(y.Name)
	})
	return out, nil
}
