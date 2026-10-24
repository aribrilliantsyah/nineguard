package pricing

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"nineguard/internal/db"
)

type CostSettings struct {
	DisplayCurrency   string  `json:"display_currency"`
	FXRateManual      string  `json:"fx_rate_manual"`
	FXRateAuto        string  `json:"fx_rate_auto"`
	FXRateSyncedAt    string  `json:"fx_rate_synced_at"`
	CostVisibleToKeys bool    `json:"cost_visible_to_keys"`
	PriceListURL      string  `json:"price_list_url"`
	PriceListSyncedAt string  `json:"price_list_synced_at"`
	EffectiveRate     float64 `json:"effective_rate"`
	EffectiveCurrency string  `json:"effective_currency"`
	EffectiveSource   string  `json:"effective_source"` // "usd", "manual", "auto", "none"
}

type CostSettingsUpdate struct {
	DisplayCurrency   *string `json:"display_currency"`
	FXRateManual      *string `json:"fx_rate_manual"`
	CostVisibleToKeys *bool   `json:"cost_visible_to_keys"`
}

type Manager struct {
	db       *db.DB
	resolver *Resolver
	fxClient *FXClient
	mu       sync.Mutex
}

func NewManager(database *db.DB) (*Manager, error) {
	mgr := &Manager{
		db:       database,
		resolver: NewResolver(nil, nil),
		fxClient: NewFXClient(""),
	}

	if err := mgr.initSnapshotIfEmpty(); err != nil {
		slog.Warn("failed to initialize prices snapshot", "error", err)
	}

	if err := mgr.ReloadResolver(); err != nil {
		return nil, fmt.Errorf("failed to reload pricing resolver: %w", err)
	}

	return mgr, nil
}

func (m *Manager) Resolver() *Resolver {
	return m.resolver
}

func (m *Manager) DB() *db.DB {
	return m.db
}

func (m *Manager) SetFXClient(client *FXClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fxClient = client
}

func (m *Manager) initSnapshotIfEmpty() error {
	var count int
	err := m.db.QueryRow("SELECT COUNT(*) FROM model_prices").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	snapshot, err := LoadSnapshot()
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %w", err)
	}

	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO model_prices (family, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for fam, p := range snapshot {
		if _, err := stmt.Exec(fam, p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheWritePerMTok); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (m *Manager) ReloadResolver() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Read model_prices
	pRows, err := m.db.Query("SELECT family, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok FROM model_prices")
	if err != nil {
		return fmt.Errorf("failed to query model_prices: %w", err)
	}

	prices := make(map[string]Price)
	for pRows.Next() {
		var fam string
		var p Price
		if err := pRows.Scan(&fam, &p.InputPerMTok, &p.OutputPerMTok, &p.CacheReadPerMTok, &p.CacheWritePerMTok); err == nil {
			prices[fam] = p
		}
	}
	pRows.Close()

	// 2. Read model_price_overrides
	oRows, err := m.db.Query("SELECT model_id, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok FROM model_price_overrides")
	if err != nil {
		return fmt.Errorf("failed to query model_price_overrides: %w", err)
	}

	overrides := make(map[string]Price)
	for oRows.Next() {
		var mdl string
		var p Price
		if err := oRows.Scan(&mdl, &p.InputPerMTok, &p.OutputPerMTok, &p.CacheReadPerMTok, &p.CacheWritePerMTok); err == nil {
			overrides[mdl] = p
		}
	}
	oRows.Close()

	m.resolver.Reload(prices, overrides)
	return nil
}

func (m *Manager) SetOverride(modelID string, inputPerMTok, outputPerMTok float64, cacheReadPerMTok, cacheWritePerMTok *float64) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("model_id cannot be empty")
	}
	if inputPerMTok < 0 || outputPerMTok < 0 {
		return fmt.Errorf("price rates cannot be negative")
	}

	_, err := m.db.Exec(`
		INSERT INTO model_price_overrides (model_id, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(model_id) DO UPDATE SET
			input_per_mtok = excluded.input_per_mtok,
			output_per_mtok = excluded.output_per_mtok,
			cache_read_per_mtok = excluded.cache_read_per_mtok,
			cache_write_per_mtok = excluded.cache_write_per_mtok,
			updated_at = CURRENT_TIMESTAMP
	`, modelID, inputPerMTok, outputPerMTok, cacheReadPerMTok, cacheWritePerMTok)
	if err != nil {
		return fmt.Errorf("failed to save override: %w", err)
	}

	return m.ReloadResolver()
}

func (m *Manager) DeleteOverride(modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("model_id cannot be empty")
	}

	res, err := m.db.Exec("DELETE FROM model_price_overrides WHERE model_id = ?", modelID)
	if err != nil {
		return fmt.Errorf("failed to delete override: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("override for %q not found", modelID)
	}

	return m.ReloadResolver()
}

func (m *Manager) ListPrices() ([]ModelPriceRow, error) {
	rows, err := m.db.Query("SELECT family, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok, updated_at FROM model_prices ORDER BY family ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ModelPriceRow
	for rows.Next() {
		var r ModelPriceRow
		if err := rows.Scan(&r.Family, &r.InputPerMTok, &r.OutputPerMTok, &r.CacheReadPerMTok, &r.CacheWritePerMTok, &r.UpdatedAt); err == nil {
			list = append(list, r)
		}
	}
	return list, nil
}

func (m *Manager) ListOverrides() ([]OverrideRow, error) {
	rows, err := m.db.Query("SELECT model_id, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok, updated_at FROM model_price_overrides ORDER BY model_id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []OverrideRow
	for rows.Next() {
		var r OverrideRow
		if err := rows.Scan(&r.ModelID, &r.InputPerMTok, &r.OutputPerMTok, &r.CacheReadPerMTok, &r.CacheWritePerMTok, &r.UpdatedAt); err == nil {
			list = append(list, r)
		}
	}
	return list, nil
}

func (m *Manager) GetCostSettings() (CostSettings, error) {
	settings := CostSettings{
		DisplayCurrency:   "USD",
		CostVisibleToKeys: true,
	}

	rows, err := m.db.Query("SELECT key, value FROM settings WHERE key IN ('display_currency', 'fx_rate_manual', 'fx_rate_auto', 'fx_rate_synced_at', 'cost_visible_to_keys', 'price_list_url', 'price_list_synced_at')")
	if err != nil {
		return settings, err
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			switch k {
			case "display_currency":
				if v != "" {
					settings.DisplayCurrency = v
				}
			case "fx_rate_manual":
				settings.FXRateManual = v
			case "fx_rate_auto":
				settings.FXRateAuto = v
			case "fx_rate_synced_at":
				settings.FXRateSyncedAt = v
			case "cost_visible_to_keys":
				settings.CostVisibleToKeys = (v != "false" && v != "0")
			case "price_list_url":
				settings.PriceListURL = v
			case "price_list_synced_at":
				settings.PriceListSyncedAt = v
			}
		}
	}

	rate, cur, src := GetEffectiveRate(settings.DisplayCurrency, settings.FXRateManual, settings.FXRateAuto, settings.DisplayCurrency)
	settings.EffectiveRate = rate
	settings.EffectiveCurrency = cur
	settings.EffectiveSource = src

	return settings, nil
}

func (m *Manager) UpdateCostSettings(upd CostSettingsUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var currencyChanged bool
	var newCur string

	if upd.DisplayCurrency != nil {
		cur := strings.ToUpper(strings.TrimSpace(*upd.DisplayCurrency))
		if cur == "" {
			cur = "USD"
		}
		var oldCur string
		_ = m.db.QueryRow("SELECT value FROM settings WHERE key = 'display_currency'").Scan(&oldCur)
		if cur != oldCur {
			currencyChanged = true
			newCur = cur
		}
		if _, err := m.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'display_currency'", cur); err != nil {
			return err
		}
	}

	if upd.FXRateManual != nil {
		val := strings.TrimSpace(*upd.FXRateManual)
		if val != "" {
			if _, err := strconv.ParseFloat(val, 64); err != nil {
				return fmt.Errorf("manual fx rate must be a valid positive number")
			}
		}
		if _, err := m.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'fx_rate_manual'", val); err != nil {
			return err
		}
	}

	if upd.CostVisibleToKeys != nil {
		val := "false"
		if *upd.CostVisibleToKeys {
			val = "true"
		}
		if _, err := m.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'cost_visible_to_keys'", val); err != nil {
			return err
		}
	}

	if currencyChanged && newCur != "USD" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = m.RefreshFX(ctx)
		}()
	}

	return nil
}

func (m *Manager) RefreshPrices(ctx context.Context) (int, error) {
	var url string
	_ = m.db.QueryRow("SELECT value FROM settings WHERE key = 'price_list_url'").Scan(&url)
	if strings.TrimSpace(url) == "" {
		url = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create prices request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch price list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("price list returned HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read price list: %w", err)
	}

	prices, err := ParseLiteLLM(data)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price list: %w", err)
	}

	tx, err := m.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM model_prices"); err != nil {
		return 0, err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO model_prices (family, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for fam, p := range prices {
		if _, err := stmt.Exec(fam, p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheWritePerMTok); err != nil {
			return 0, err
		}
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'price_list_synced_at'", nowStr); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	_ = m.ReloadResolver()
	return len(prices), nil
}

func (m *Manager) RefreshFX(ctx context.Context) (float64, error) {
	settings, err := m.GetCostSettings()
	if err != nil {
		return 1.0, err
	}

	if settings.DisplayCurrency == "USD" {
		return 1.0, nil
	}

	rate, _, err := m.fxClient.FetchRate(ctx, settings.DisplayCurrency)
	if err != nil {
		slog.Warn("failed to fetch fx exchange rate", "currency", settings.DisplayCurrency, "error", err)
		return 1.0, err
	}

	rateStr := fmt.Sprintf("%.4f", rate)
	nowStr := time.Now().UTC().Format(time.RFC3339)

	_, _ = m.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'fx_rate_auto'", rateStr)
	_, _ = m.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'fx_rate_synced_at'", nowStr)

	return rate, nil
}
