package keys

import (
	"fmt"
	"strings"
)

// ListOptions controls paging, sorting, and filtering of GET /api/v1/keys?page=.
type ListOptions struct {
	Page   int    // 1-based
	Limit  int    // one of AllowedLimits
	Sort   string // one of SortFields
	Order  string // "asc" | "desc"
	Query  string // case-insensitive substring of key name
	Status string // "all" | "active" | "disabled"
	Mode   string // "any" | "all" | "group" | "custom"
}

// KeyPage is one page of keys plus the total number of keys matching the filters.
type KeyPage struct {
	Keys  []KeyInfo `json:"keys"`
	Total int       `json:"total"`
	Page  int       `json:"page"`
	Limit int       `json:"limit"`
}

// Allowed values, exported so the handler can list them in 400 messages.
var (
	AllowedLimits = []int{10, 25, 50, 100}
	SortFields    = []string{"name", "status", "created", "last_active", "requests", "tokens"}
	SortOrders    = []string{"asc", "desc"}
	StatusFilters = []string{"all", "active", "disabled"}
	ModeFilters   = []string{"any", "all", "group", "custom"}
)

// sortColumns maps a sort field to its SQL expression. usedFlag is true for
// fields where never-used keys must sort last regardless of order.
var sortColumns = map[string]struct {
	expr     string
	usedLast bool
}{
	"name":        {"k.name COLLATE NOCASE", false},
	"status":      {"k.is_active", false},
	"created":     {"k.created_at", false},
	"last_active": {"s.last_used_at", true},
	"requests":    {"COALESCE(s.total_requests, 0)", true},
	"tokens":      {"COALESCE(s.total_tokens, 0)", true},
}

// ClampLimit returns the allowed page size nearest to n (ties go to the smaller).
// Zero or negative values return the default, 25.
func ClampLimit(n int) int {
	if n <= 0 {
		return 25
	}
	best := AllowedLimits[0]
	for _, l := range AllowedLimits {
		if abs(n-l) < abs(n-best) {
			best = l
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// Normalize fills defaults and validates every field. It returns an error
// naming the invalid parameter and its allowed values.
func (o *ListOptions) Normalize() error {
	if o.Page < 1 {
		o.Page = 1
	}
	o.Limit = ClampLimit(o.Limit)
	defaults := []struct {
		name    string
		val     *string
		def     string
		allowed []string
	}{
		{"sort", &o.Sort, "last_active", SortFields},
		{"order", &o.Order, "desc", SortOrders},
		{"status", &o.Status, "all", StatusFilters},
		{"mode", &o.Mode, "any", ModeFilters},
	}
	for _, d := range defaults {
		*d.val = strings.ToLower(strings.TrimSpace(*d.val))
		if *d.val == "" {
			*d.val = d.def
		}
		if !oneOf(*d.val, d.allowed) {
			return fmt.Errorf("invalid %s %q: allowed values are %s", d.name, *d.val, strings.Join(d.allowed, ", "))
		}
	}
	o.Query = strings.TrimSpace(o.Query)
	return nil
}

// likeEscape escapes LIKE wildcards so user input matches literally (ESCAPE '\').
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ListKeysPage returns one page of keys with all-time stats. Sorting, filtering
// and paging happen in SQL. Never-used keys sort last for last_active,
// requests and tokens in both directions; ties break by created_at DESC, id.
func (m *Manager) ListKeysPage(opts ListOptions) (*KeyPage, error) {
	if err := opts.Normalize(); err != nil {
		return nil, err
	}

	var conds []string
	var args []any
	if opts.Query != "" {
		conds = append(conds, `k.name LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(opts.Query)+"%")
	}
	switch opts.Status {
	case "active":
		conds = append(conds, "k.is_active = 1")
	case "disabled":
		conds = append(conds, "k.is_active = 0")
	}
	if opts.Mode != "any" {
		conds = append(conds, "COALESCE(NULLIF(k.model_access_mode, ''), 'all') = ?")
		args = append(args, opts.Mode)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM api_keys k"+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	col := sortColumns[opts.Sort]
	dir := "ASC"
	if opts.Order == "desc" {
		dir = "DESC"
	}
	order := ""
	if col.usedLast {
		order = "(s.api_key_id IS NULL) ASC, "
	}
	order += fmt.Sprintf("%s %s, k.created_at DESC, k.id ASC", col.expr, dir)

	query := "SELECT " + keyStatsColumns + keyStatsFrom + where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	pageArgs := append(append([]any{}, args...), opts.Limit, (opts.Page-1)*opts.Limit)
	rows, err := m.db.Query(query, pageArgs...)
	if err != nil {
		return nil, err
	}
	list, err := m.scanKeyRows(rows)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	m.enrichQuotaUsage(list)
	return &KeyPage{Keys: list, Total: total, Page: opts.Page, Limit: opts.Limit}, nil
}
