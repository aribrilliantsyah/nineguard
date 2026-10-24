package keys_test

import (
	"testing"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

// List rows and single reads carry the Key Quota's next reset as an absolute instant, so the keys
// table can show when it resets without one extra request per row (ADR 0010 #9).
func TestKeyReadsCarryQuotaResetAt(t *testing.T) {
	mgr, _ := newMgr(t)
	k, _ := mgr.CreateKeyWithOptions("a", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "daily", ResetSchedule: jkt("14:00", 1, 1)})
	free, _ := mgr.CreateKey("free", "all", nil, nil)
	total, _ := mgr.CreateKeyWithOptions("life", keys.CreateKeyOptions{QuotaLimit: 1000, QuotaPeriod: "total"})

	_, want := traffic.CalcQuotaWindow("daily", mustRule(t, mgr, k.ID), time.Now())
	wantStr := want.UTC().Format(time.RFC3339)

	one, _ := mgr.GetKey(k.ID)
	if one.QuotaResetAt == nil || *one.QuotaResetAt != wantStr {
		t.Errorf("GetKey reset = %v, want %s", one.QuotaResetAt, wantStr)
	}
	list, _ := mgr.ListKeys()
	page, _ := mgr.ListKeysPage(keys.ListOptions{Page: 1, Limit: 50})
	for name, ks := range map[string][]keys.KeyInfo{"ListKeys": list, "ListKeysPage": page.Keys} {
		for _, ki := range ks {
			switch ki.ID {
			case k.ID:
				if ki.QuotaResetAt == nil || *ki.QuotaResetAt != wantStr {
					t.Errorf("%s reset = %v, want %s", name, ki.QuotaResetAt, wantStr)
				}
			case free.ID:
				if ki.QuotaResetAt != nil {
					t.Errorf("%s: a key without a quota has no reset, got %v", name, *ki.QuotaResetAt)
				}
			case total.ID:
				if ki.QuotaResetAt != nil {
					t.Errorf("%s: a lifetime quota never resets, got %v", name, *ki.QuotaResetAt)
				}
			}
		}
	}
}

func mustRule(t *testing.T, mgr *keys.Manager, id string) traffic.ResetRule {
	t.Helper()
	ki, err := mgr.GetKey(id)
	if err != nil {
		t.Fatal(err)
	}
	return ki.ResetRule
}
