package syslog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"nineguard/internal/db"
)

type SystemLogEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	Attrs     string    `json:"attrs,omitempty"`
}

type FilterParams struct {
	Period    string
	StartDate string
	EndDate   string
	From      string
	To        string
	Source    string
	Level     string
	Search    string
	Limit     int
	Offset    int
	Cursor    string
}

type VolumeBucket struct {
	Start  int64            `json:"start"` // unix nano
	Counts map[string]int64 `json:"counts"`
}

type VolumeResult struct {
	From        int64            `json:"from"` // unix nano
	To          int64            `json:"to"` // unix nano
	BucketNanos int64            `json:"bucket_nanos"`
	Buckets     []VolumeBucket   `json:"buckets"`
	Totals      map[string]int64 `json:"totals"`
}

type Manager struct {
	db     *db.DB
	ch     chan SystemLogEntry
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewManager(database *db.DB) *Manager {
	m := &Manager{
		db:     database,
		ch:     make(chan SystemLogEntry, 2048),
		stopCh: make(chan struct{}),
	}
	m.wg.Add(1)
	go m.worker()
	return m
}

func (m *Manager) Close() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *Manager) worker() {
	defer m.wg.Done()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var batch []SystemLogEntry

	flush := func() {
		if len(batch) == 0 {
			return
		}
		tx, err := m.db.Begin()
		if err != nil {
			batch = batch[:0]
			return
		}
		stmt, err := tx.Prepare(`
			INSERT INTO system_logs (timestamp, level, source, message, attrs)
			VALUES (?, ?, ?, ?, ?)
		`)
		if err != nil {
			_ = tx.Rollback()
			batch = batch[:0]
			return
		}
		defer stmt.Close()

		for _, e := range batch {
			ts := e.Timestamp
			if ts.IsZero() {
				ts = time.Now().UTC()
			}
			_, _ = stmt.Exec(ts.Format("2006-01-02 15:04:05"), e.Level, e.Source, e.Message, e.Attrs)
		}
		_ = tx.Commit()
		batch = batch[:0]
	}

	for {
		select {
		case <-m.stopCh:
			// Drain remaining
			for {
				select {
				case e := <-m.ch:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		case e := <-m.ch:
			batch = append(batch, e)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (m *Manager) Record(e SystemLogEntry) {
	if e.Level == "" {
		e.Level = "INFO"
	}
	e.Level = NormalizeLevel(e.Level)
	if e.Source == "" {
		e.Source = "gateway"
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	select {
	case m.ch <- e:
	default:
		// Queue full, drop rather than stall system
	}
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

func (m *Manager) QueryLogs(p FilterParams) ([]SystemLogEntry, int, error) {
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
		if p.StartDate != "" && p.EndDate != "" {
			conditions = append(conditions, "timestamp >= datetime(?) AND timestamp <= datetime(?)")
			args = append(args, p.StartDate+" 00:00:00", p.EndDate+" 23:59:59")
		} else {
			switch p.Period {
			case "yesterday":
				conditions = append(conditions, "timestamp >= datetime('now', '-1 day', 'start of day') AND timestamp < date('now', 'start of day')")
			case "7d":
				conditions = append(conditions, "timestamp >= datetime('now', '-7 days')")
			case "30d":
				conditions = append(conditions, "timestamp >= datetime('now', '-30 days')")
			case "all":
				// no date filter
			default: // today
				conditions = append(conditions, "timestamp >= date('now', 'start of day')")
			}
		}
	}

	if p.Source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, p.Source)
	}

	if p.Level != "" {
		levels := strings.Split(p.Level, ",")
		var levelHolders []string
		var validLevels []string
		for _, l := range levels {
			l = NormalizeLevel(l)
			if l != "" {
				validLevels = append(validLevels, l)
				levelHolders = append(levelHolders, "?")
			}
		}
		if len(validLevels) > 0 && len(validLevels) < 5 {
			cond := fmt.Sprintf("UPPER(level) IN (%s)", strings.Join(levelHolders, ","))
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
				case "source":
					conditions = append(conditions, "source LIKE ?")
					args = append(args, "%"+val+"%")
					continue
				case "level":
					conditions = append(conditions, "UPPER(level) = ?")
					args = append(args, NormalizeLevel(val))
					continue
				}
			}
			likeTerm := "%" + term + "%"
			conditions = append(conditions, "(message LIKE ? OR source LIKE ? OR COALESCE(attrs, '') LIKE ?)")
			args = append(args, likeTerm, likeTerm, likeTerm)
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

	countQuery := "SELECT COUNT(*) FROM system_logs " + whereClause
	var total int
	err := m.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 500 {
		p.Limit = 500
	}

	query := fmt.Sprintf(`
		SELECT id, timestamp, level, source, message, COALESCE(attrs, '')
		FROM system_logs
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

	var list []SystemLogEntry
	for rows.Next() {
		var e SystemLogEntry
		var attrs sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Level, &e.Source, &e.Message, &attrs); err != nil {
			return nil, 0, err
		}
		if attrs.Valid {
			e.Attrs = attrs.String
		}
		e.Level = NormalizeLevel(e.Level)
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
		if p.StartDate != "" && p.EndDate != "" {
			t1, _ := time.Parse("2006-01-02", p.StartDate)
			t2, _ := time.Parse("2006-01-02", p.EndDate)
			from = t1.UTC()
			to = t2.Add(24*time.Hour - time.Second).UTC()
		} else {
			switch p.Period {
			case "7d":
				from = to.Add(-7 * 24 * time.Hour)
			case "30d":
				from = to.Add(-30 * 24 * time.Hour)
			case "yesterday":
				yest := to.AddDate(0, 0, -1)
				from = time.Date(yest.Year(), yest.Month(), yest.Day(), 0, 0, 0, 0, time.UTC)
				to = time.Date(yest.Year(), yest.Month(), yest.Day(), 23, 59, 59, 0, time.UTC)
			default:
				from = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
				to = from.Add(24*time.Hour - time.Second)
			}
		}
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
			"DEBUG": 0, "INFO": 0, "WARN": 0, "ERROR": 0, "FATAL": 0,
		},
	}
	for i := range res.Buckets {
		res.Buckets[i] = VolumeBucket{
			Start:  (fromUnix + int64(i)*bucketWidthSec) * 1e9,
			Counts: map[string]int64{"DEBUG": 0, "INFO": 0, "WARN": 0, "ERROR": 0, "FATAL": 0},
		}
	}

	var conditions []string
	var args []interface{}

	conditions = append(conditions, "timestamp >= datetime(?)")
	args = append(args, from.Format("2006-01-02 15:04:05"))

	conditions = append(conditions, "timestamp <= datetime(?)")
	args = append(args, to.Format("2006-01-02 15:04:05"))

	if p.Source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, p.Source)
	}
	if p.Level != "" {
		levels := strings.Split(p.Level, ",")
		var levelHolders []string
		var validLevels []string
		for _, l := range levels {
			l = NormalizeLevel(l)
			if l != "" {
				validLevels = append(validLevels, l)
				levelHolders = append(levelHolders, "?")
			}
		}
		if len(validLevels) > 0 && len(validLevels) < 5 {
			cond := fmt.Sprintf("UPPER(level) IN (%s)", strings.Join(levelHolders, ","))
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
			likeTerm := "%" + term + "%"
			conditions = append(conditions, "(message LIKE ? OR source LIKE ? OR COALESCE(attrs, '') LIKE ?)")
			args = append(args, likeTerm, likeTerm, likeTerm)
		}
	}

	query := fmt.Sprintf(`
		SELECT 
			(CAST(strftime('%%s', timestamp) AS INTEGER) - %d) / %d as b_idx,
			UPPER(level) as lvl,
			COUNT(*) as cnt
		FROM system_logs
		WHERE %s
		GROUP BY b_idx, lvl
	`, fromUnix, bucketWidthSec, strings.Join(conditions, " AND "))

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var bIdx int
		var lvl string
		var cnt int64
		if err := rows.Scan(&bIdx, &lvl, &cnt); err == nil {
			if bIdx < 0 {
				bIdx = 0
			}
			if bIdx >= buckets {
				bIdx = buckets - 1
			}
			lvl = NormalizeLevel(lvl)
			res.Buckets[bIdx].Counts[lvl] += cnt
			res.Totals[lvl] += cnt
		}
	}

	return res, nil
}

func (m *Manager) GetSources() ([]string, error) {
	rows, err := m.db.Query("SELECT DISTINCT source FROM system_logs ORDER BY source ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil && s != "" {
			sources = append(sources, s)
		}
	}
	return sources, nil
}

// ── Slog Integration ──

type SlogHandler struct {
	mgr  *Manager
	next slog.Handler
}

func NewSlogHandler(mgr *Manager, next slog.Handler) *SlogHandler {
	return &SlogHandler{mgr: mgr, next: next}
}

func (h *SlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *SlogHandler) Handle(ctx context.Context, r slog.Record) error {
	// First delegate to original handler (stdout)
	err := h.next.Handle(ctx, r)

	// Determine level
	lvl := "INFO"
	switch {
	case r.Level < slog.LevelInfo:
		lvl = "DEBUG"
	case r.Level < slog.LevelWarn:
		lvl = "INFO"
	case r.Level < slog.LevelError:
		lvl = "WARN"
	case r.Level >= 12: // fatal
		lvl = "FATAL"
	default:
		lvl = "ERROR"
	}

	source := "server"
	attrsMap := make(map[string]interface{})

	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "source" {
			source = a.Value.String()
		} else {
			attrsMap[a.Key] = a.Value.Any()
		}
		return true
	})

	var attrsJSON string
	if len(attrsMap) > 0 {
		b, _ := json.Marshal(attrsMap)
		attrsJSON = string(b)
	}

	h.mgr.Record(SystemLogEntry{
		Timestamp: r.Time.UTC(),
		Level:     lvl,
		Source:    source,
		Message:   r.Message,
		Attrs:     attrsJSON,
	})

	return err
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SlogHandler{mgr: h.mgr, next: h.next.WithAttrs(attrs)}
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	return &SlogHandler{mgr: h.mgr, next: h.next.WithGroup(name)}
}
