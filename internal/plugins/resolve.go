package plugins

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"nineguard/internal/models"
)

// MatchModelPattern evaluates whether model matches a given pattern.
func MatchModelPattern(pattern, model string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "*" || pattern == "" || strings.EqualFold(pattern, "all") {
		return true
	}
	if strings.EqualFold(pattern, model) {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		if strings.HasPrefix(strings.ToLower(model), strings.ToLower(prefix)+"/") {
			return true
		}
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*")
		if strings.HasSuffix(strings.ToLower(model), strings.ToLower(suffix)) {
			return true
		}
	}
	if strings.Contains(model, "/") && !strings.Contains(pattern, "/") {
		parts := strings.SplitN(model, "/", 2)
		if strings.EqualFold(parts[1], pattern) {
			return true
		}
	}
	if strings.Contains(pattern, "/") && !strings.Contains(model, "/") {
		parts := strings.SplitN(pattern, "/", 2)
		if strings.EqualFold(parts[1], model) {
			return true
		}
	}
	return false
}

// ScopeLabel produces standard scope origin labels conforming to spec §9.2.
func ScopeLabel(scopeType ScopeType, scopeID, name string) ScopeOrigin {
	switch scopeType {
	case ScopeGlobal:
		return ScopeOrigin{
			ScopeType: ScopeGlobal,
			ScopeID:   "",
			Label:     "All keys, all models",
		}
	case ScopeGroup:
		disp := name
		if disp == "" {
			disp = scopeID
		}
		return ScopeOrigin{
			ScopeType: ScopeGroup,
			ScopeID:   scopeID,
			Label:     fmt.Sprintf("Models in %q", disp),
		}
	case ScopeKey:
		disp := name
		if disp == "" {
			disp = scopeID
		}
		return ScopeOrigin{
			ScopeType: ScopeKey,
			ScopeID:   scopeID,
			Label:     fmt.Sprintf("Key %q", disp),
		}
	default:
		return ScopeOrigin{
			ScopeType: scopeType,
			ScopeID:   scopeID,
			Label:     name,
		}
	}
}

func mergeJSON(target map[string]any, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err == nil {
		for k, v := range m {
			target[k] = v
		}
	}
}

type groupCandidate struct {
	group   models.ModelGroup
	binding Binding
}

// ResolvePluginsForRequest calculates effective plugins, states, merged settings, and scope origins.
func ResolvePluginsForRequest(
	allPlugins []Plugin,
	allBindings []Binding,
	groups []models.ModelGroup,
	apiKeyID, apiKeyName string,
	model string,
) []ResolvedPlugin {
	// Index bindings by plugin_id -> scope_type -> scope_id
	bindingMap := make(map[string]map[ScopeType]map[string]Binding)
	for _, b := range allBindings {
		if _, ok := bindingMap[b.PluginID]; !ok {
			bindingMap[b.PluginID] = make(map[ScopeType]map[string]Binding)
		}
		if _, ok := bindingMap[b.PluginID][b.ScopeType]; !ok {
			bindingMap[b.PluginID][b.ScopeType] = make(map[string]Binding)
		}
		bindingMap[b.PluginID][b.ScopeType][b.ScopeID] = b
	}

	groupByID := make(map[string]models.ModelGroup)
	for _, g := range groups {
		groupByID[g.ID] = g
	}

	result := make([]ResolvedPlugin, 0, len(allPlugins))

	for _, p := range allPlugins {
		mergedSettings := make(map[string]any)
		mergeJSON(mergedSettings, p.DefaultSettings)

		effectiveState := StateOff
		decidedBy := ScopeLabel(ScopeGlobal, "", "")
		var overridden []ScopeOrigin

		// 1. Global Scope
		if gBindings, ok := bindingMap[p.ID][ScopeGlobal]; ok {
			if glob, exists := gBindings[""]; exists {
				if glob.State != StateInherit {
					effectiveState = glob.State
				}
				mergeJSON(mergedSettings, glob.Settings)
			}
		}

		// 2. Model Groups Scope
		// Find matching groups whose models cover the requested model
		var candidates []groupCandidate
		for _, g := range groups {
			matches := false
			for _, pat := range g.Models {
				if MatchModelPattern(pat, model) {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}

			// Check if this group has a non-inherit binding for this plugin
			if grpBindings, ok := bindingMap[p.ID][ScopeGroup]; ok {
				if b, exists := grpBindings[g.ID]; exists && b.State != StateInherit {
					candidates = append(candidates, groupCandidate{group: g, binding: b})
				}
			}
		}

		if len(candidates) > 0 {
			// Sort candidates: Priority DESC, Name ASC
			sort.Slice(candidates, func(i, j int) bool {
				if candidates[i].group.Priority != candidates[j].group.Priority {
					return candidates[i].group.Priority > candidates[j].group.Priority
				}
				return candidates[i].group.Name < candidates[j].group.Name
			})

			winner := candidates[0]
			effectiveState = winner.binding.State
			decidedBy = ScopeLabel(ScopeGroup, winner.group.ID, winner.group.Name)
			mergeJSON(mergedSettings, winner.binding.Settings)

			for _, loser := range candidates[1:] {
				overridden = append(overridden, ScopeLabel(ScopeGroup, loser.group.ID, loser.group.Name))
			}
		}

		// 3. API Key Scope
		if apiKeyID != "" {
			if keyBindings, ok := bindingMap[p.ID][ScopeKey]; ok {
				if kb, exists := keyBindings[apiKeyID]; exists && kb.State != StateInherit {
					effectiveState = kb.State
					decidedBy = ScopeLabel(ScopeKey, apiKeyID, apiKeyName)
					mergeJSON(mergedSettings, kb.Settings)
				}
			}
		}

		result = append(result, ResolvedPlugin{
			Plugin:         p,
			EffectiveState: effectiveState,
			MergedSettings: mergedSettings,
			DecidedBy:      decidedBy,
			Overridden:     overridden,
		})
	}

	return result
}
