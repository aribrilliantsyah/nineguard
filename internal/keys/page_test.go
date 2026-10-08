package keys_test

import (
	"strings"
	"testing"

	"nineguard/internal/keys"
)

// seedPaging creates 5 keys:
//
//	id  name      mode    active  created     requests tokens last_used
//	k1  Alpha     all     yes     10-01       3        300    10-06 09:00
//	k2  bravo     group   yes     10-02       1        900    10-06 12:00
//	k3  Charlie   custom  no      10-03       0        0      never
//	k4  delta     all     yes     10-04       0        0      never
//	k5  50%_off   group   no      10-05       2        100    10-01 00:00
func seedPaging(t *testing.T) *keys.Manager {
	t.Helper()
	database, km := newKeysDB(t)
	addKey(t, database, "k1", "Alpha", "all", true, "2026-10-01 00:00:00")
	addKey(t, database, "k2", "bravo", "group", true, "2026-10-02 00:00:00")
	addKey(t, database, "k3", "Charlie", "custom", false, "2026-10-03 00:00:00")
	addKey(t, database, "k4", "delta", "all", true, "2026-10-04 00:00:00")
	addKey(t, database, "k5", "50%_off", "group", false, "2026-10-05 00:00:00")
	addTraffic(t, database, "k1", "2026-10-06 09:00:00", 200, 100)
	addTraffic(t, database, "k1", "2026-10-05 09:00:00", 200, 100)
	addTraffic(t, database, "k1", "2026-10-04 09:00:00", 403, 100)
	addTraffic(t, database, "k2", "2026-10-06 12:00:00", 200, 900)
	addTraffic(t, database, "k5", "2026-10-01 00:00:00", 200, 50)
	addTraffic(t, database, "k5", "2026-09-30 00:00:00", 200, 50)
	return km
}

func page(t *testing.T, km *keys.Manager, o keys.ListOptions) *keys.KeyPage {
	t.Helper()
	p, err := km.ListKeysPage(o)
	if err != nil {
		t.Fatalf("ListKeysPage(%+v): %v", o, err)
	}
	return p
}

func TestListKeysPageSorting(t *testing.T) {
	km := seedPaging(t)
	cases := []struct {
		sort, order, want string
	}{
		// Never-used keys (k3, k4) last in both directions; tie-break created_at DESC.
		{"last_active", "desc", "[k2 k1 k5 k4 k3]"},
		{"last_active", "asc", "[k5 k1 k2 k4 k3]"},
		{"requests", "desc", "[k1 k5 k2 k4 k3]"},
		{"requests", "asc", "[k2 k5 k1 k4 k3]"},
		{"tokens", "desc", "[k2 k1 k5 k4 k3]"},
		{"tokens", "asc", "[k5 k1 k2 k4 k3]"},
		// Case-insensitive name sort.
		{"name", "asc", "[k5 k1 k2 k3 k4]"},
		{"name", "desc", "[k4 k3 k2 k1 k5]"},
		{"created", "asc", "[k1 k2 k3 k4 k5]"},
		{"created", "desc", "[k5 k4 k3 k2 k1]"},
		// Disabled (0) first ascending; ties by created_at DESC.
		{"status", "asc", "[k5 k3 k4 k2 k1]"},
		{"status", "desc", "[k4 k2 k1 k5 k3]"},
	}
	for _, c := range cases {
		p := page(t, km, keys.ListOptions{Page: 1, Limit: 25, Sort: c.sort, Order: c.order})
		if got := idsStr(p.Keys); got != c.want {
			t.Errorf("sort=%s order=%s: got %s, want %s", c.sort, c.order, got, c.want)
		}
		if p.Total != 5 {
			t.Errorf("total = %d", p.Total)
		}
	}
}

func TestListKeysPageDefaultsAndPaging(t *testing.T) {
	km := seedPaging(t)
	p := page(t, km, keys.ListOptions{})
	if p.Page != 1 || p.Limit != 25 || idsStr(p.Keys) != "[k2 k1 k5 k4 k3]" {
		t.Errorf("defaults: page %d limit %d keys %s", p.Page, p.Limit, idsStr(p.Keys))
	}

	p = page(t, km, keys.ListOptions{Page: 1, Limit: 10, Sort: "created", Order: "asc"})
	if len(p.Keys) != 5 {
		t.Errorf("limit 10 page 1: %d keys", len(p.Keys))
	}
	p = page(t, km, keys.ListOptions{Page: 2, Limit: 10})
	if len(p.Keys) != 0 || p.Total != 5 || p.Page != 2 {
		t.Errorf("beyond last page: %d keys total %d page %d", len(p.Keys), p.Total, p.Page)
	}
	p = page(t, km, keys.ListOptions{Page: -3})
	if p.Page != 1 {
		t.Errorf("negative page normalized to %d", p.Page)
	}
}

func TestClampLimit(t *testing.T) {
	cases := map[int]int{0: 25, -1: 25, 1: 10, 10: 10, 17: 10, 18: 25, 25: 25, 37: 25, 38: 50, 74: 50, 76: 100, 500: 100}
	for in, want := range cases {
		if got := keys.ClampLimit(in); got != want {
			t.Errorf("ClampLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestListKeysPageFilters(t *testing.T) {
	km := seedPaging(t)
	cases := []struct {
		opts keys.ListOptions
		want string
	}{
		{keys.ListOptions{Query: "AL", Sort: "created", Order: "asc"}, "[k1]"}, // case-insensitive substring
		{keys.ListOptions{Query: "%", Sort: "created", Order: "asc"}, "[k5]"},  // % matched literally
		{keys.ListOptions{Query: "_", Sort: "created", Order: "asc"}, "[k5]"},  // _ matched literally
		{keys.ListOptions{Status: "active", Sort: "created", Order: "asc"}, "[k1 k2 k4]"},
		{keys.ListOptions{Status: "disabled", Sort: "created", Order: "asc"}, "[k3 k5]"},
		{keys.ListOptions{Mode: "group", Sort: "created", Order: "asc"}, "[k2 k5]"},
		{keys.ListOptions{Mode: "all", Status: "active", Sort: "created", Order: "asc"}, "[k1 k4]"},
		{keys.ListOptions{Query: "zzz"}, "[]"},
	}
	for _, c := range cases {
		p := page(t, km, c.opts)
		if got := idsStr(p.Keys); got != c.want {
			t.Errorf("%+v: got %s, want %s", c.opts, got, c.want)
		}
		if p.Total != len(p.Keys) {
			t.Errorf("%+v: total %d != len %d", c.opts, p.Total, len(p.Keys))
		}
	}
}

func TestListKeysPageInvalidParams(t *testing.T) {
	km := seedPaging(t)
	for _, o := range []keys.ListOptions{
		{Sort: "bogus"}, {Order: "sideways"}, {Status: "maybe"}, {Mode: "partial"},
	} {
		_, err := km.ListKeysPage(o)
		if err == nil || !strings.Contains(err.Error(), "allowed values are") {
			t.Errorf("%+v: err = %v", o, err)
		}
	}
}
