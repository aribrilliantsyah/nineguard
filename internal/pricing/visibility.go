package pricing

import "strings"

// ShowCost evaluates whether estimated cost should be exposed to key holders per Spec §5.5:
// ShowCost(key) = key.cost_visibility == 'show' || (key.cost_visibility IS NULL && setting cost_visible_to_keys)
func ShowCost(keyCostVisibility *string, globalCostVisible bool) bool {
	if keyCostVisibility != nil {
		v := strings.ToLower(strings.TrimSpace(*keyCostVisibility))
		if v == "show" {
			return true
		}
		if v == "hide" {
			return false
		}
	}
	return globalCostVisible
}
