package plugins

import (
	"testing"

	"nineguard/internal/models"
)

func TestResolution_ReferenceScenario(t *testing.T) {
	groups := []models.ModelGroup{
		{ID: "grp_coding", Name: "Coding", Priority: 0, Models: []string{"office/claude-sonnet-4", "office/gpt-5", "9r/claude-opus-4", "9r/qwen3-coder"}},
		{ID: "grp_roleplay", Name: "Roleplay", Priority: 0, Models: []string{"9r/claude-opus-4", "9r/deepseek-v3"}},
		{ID: "grp_demanding", Name: "Demanding", Priority: 10, Models: []string{"office/claude-sonnet-4", "9r/claude-opus-4"}},
	}

	pluginsList := []Plugin{
		{ID: "headroom", Kind: KindBuiltin, Category: CategoryInputCompression, PipelineOrder: 10, DefaultSettings: "{}"},
		{ID: "ponytail", Kind: KindBuiltin, Category: CategoryOutputStyle, PipelineOrder: 20, DefaultSettings: "{}"},
		{ID: "caveman", Kind: KindBuiltin, Category: CategoryOutputStyle, PipelineOrder: 30, DefaultSettings: "{}"},
	}

	bindings := []Binding{
		{PluginID: "headroom", ScopeType: ScopeGlobal, ScopeID: "", State: StateOff},
		{PluginID: "caveman", ScopeType: ScopeGlobal, ScopeID: "", State: StateOff},
		{PluginID: "ponytail", ScopeType: ScopeGlobal, ScopeID: "", State: StateOff},
		{PluginID: "headroom", ScopeType: ScopeGroup, ScopeID: "grp_demanding", State: StateOn},
		{PluginID: "caveman", ScopeType: ScopeKey, ScopeID: "key_pidev", State: StateOn},
	}

	keyNames := map[string]string{
		"key_pidev":    "pi-dev",
		"key_marinara": "marinara",
	}

	// Case 1: pi-dev -> office/claude-sonnet-4 -> Headroom, Caveman
	resolved := ResolvePluginsForRequest(pluginsList, bindings, groups, "key_pidev", keyNames["key_pidev"], "office/claude-sonnet-4")
	var onPlugins []string
	for _, r := range resolved {
		if r.EffectiveState == StateOn {
			onPlugins = append(onPlugins, r.Plugin.ID)
		}
	}
	if len(onPlugins) != 2 || onPlugins[0] != "headroom" || onPlugins[1] != "caveman" {
		t.Fatalf("case 1: expected [headroom, caveman], got %v", onPlugins)
	}

	// Verify DecidedBy
	for _, r := range resolved {
		if r.Plugin.ID == "headroom" && r.DecidedBy.Label != `Models in "Demanding"` {
			t.Errorf("headroom decided_by expected 'Models in \"Demanding\"', got %q", r.DecidedBy.Label)
		}
		if r.Plugin.ID == "caveman" && r.DecidedBy.Label != `Key "pi-dev"` {
			t.Errorf("caveman decided_by expected 'Key \"pi-dev\"', got %q", r.DecidedBy.Label)
		}
	}

	// Case 2: marinara -> 9r/claude-opus-4 -> Headroom
	resolved2 := ResolvePluginsForRequest(pluginsList, bindings, groups, "key_marinara", keyNames["key_marinara"], "9r/claude-opus-4")
	var onPlugins2 []string
	for _, r := range resolved2 {
		if r.EffectiveState == StateOn {
			onPlugins2 = append(onPlugins2, r.Plugin.ID)
		}
	}
	if len(onPlugins2) != 1 || onPlugins2[0] != "headroom" {
		t.Fatalf("case 2: expected [headroom], got %v", onPlugins2)
	}

	// Case 3: marinara has key override headroom = off -> nothing runs
	bindingsWithOverride := append(bindings, Binding{
		PluginID: "headroom", ScopeType: ScopeKey, ScopeID: "key_marinara", State: StateOff,
	})
	resolved3 := ResolvePluginsForRequest(pluginsList, bindingsWithOverride, groups, "key_marinara", keyNames["key_marinara"], "9r/claude-opus-4")
	for _, r := range resolved3 {
		if r.EffectiveState == StateOn {
			t.Fatalf("case 3: expected no plugins on, but %s was on", r.Plugin.ID)
		}
		if r.Plugin.ID == "headroom" && r.DecidedBy.Label != `Key "marinara"` {
			t.Errorf("expected decided by Key marinara, got %q", r.DecidedBy.Label)
		}
	}
}

func TestSettingsMergeOrder(t *testing.T) {
	plugin := Plugin{
		ID:              "custom",
		DefaultSettings: `{"mode":"fast","count":1}`,
	}
	bindings := []Binding{
		{PluginID: "custom", ScopeType: ScopeGlobal, Settings: `{"count":2,"label":"glob"}`},
		{PluginID: "custom", ScopeType: ScopeGroup, ScopeID: "grp_1", Settings: `{"label":"grp"}`},
		{PluginID: "custom", ScopeType: ScopeKey, ScopeID: "key_1", Settings: `{"count":5}`},
	}
	groups := []models.ModelGroup{
		{ID: "grp_1", Name: "G1", Priority: 1, Models: []string{"gpt-4"}},
	}

	resolved := ResolvePluginsForRequest([]Plugin{plugin}, bindings, groups, "key_1", "Key 1", "gpt-4")
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved plugin")
	}
	settings := resolved[0].MergedSettings
	if settings["mode"] != "fast" { // from default
		t.Errorf("expected mode=fast, got %v", settings["mode"])
	}
	if settings["label"] != "grp" { // from group overriding global
		t.Errorf("expected label=grp, got %v", settings["label"])
	}
	if settings["count"] != float64(5) && settings["count"] != 5 { // from key overriding group
		t.Errorf("expected count=5, got %v", settings["count"])
	}
}
