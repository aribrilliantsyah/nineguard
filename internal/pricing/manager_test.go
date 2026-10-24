package pricing_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/pricing"
)

func TestPricingManager_InitSnapshotAndOverrides(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer database.Close()

	mgr, err := pricing.NewManager(database)
	if err != nil {
		t.Fatalf("failed to create pricing manager: %v", err)
	}

	// 1. Resolver should have seeded prices from snapshot
	p, source, found := mgr.Resolver().PriceFor("claude-3-5-sonnet")
	if !found || source != "list" || p == nil {
		t.Fatalf("expected seeded price for claude-3-5-sonnet, got found=%v, source=%v", found, source)
	}

	// 2. Add an override
	cr := 0.2
	err = mgr.SetOverride("custom/override-model", 2.0, 8.0, &cr, nil)
	if err != nil {
		t.Fatalf("failed to set override: %v", err)
	}

	p, source, found = mgr.Resolver().PriceFor("custom/override-model")
	if !found || source != "override" || p.InputPerMTok != 2.0 {
		t.Fatalf("expected override price after SetOverride, got found=%v, source=%v, p=%+v", found, source, p)
	}

	// 3. Delete override
	err = mgr.DeleteOverride("custom/override-model")
	if err != nil {
		t.Fatalf("failed to delete override: %v", err)
	}

	_, _, found = mgr.Resolver().PriceFor("custom/override-model")
	if found {
		t.Fatalf("expected override to be deleted")
	}

	// 4. Cost Settings
	settings, err := mgr.GetCostSettings()
	if err != nil {
		t.Fatalf("failed to get cost settings: %v", err)
	}
	if settings.DisplayCurrency != "USD" {
		t.Fatalf("expected USD default display currency, got %s", settings.DisplayCurrency)
	}
	if !settings.CostVisibleToKeys {
		t.Fatalf("expected cost_visible_to_keys default true")
	}
}
