package plugins

import (
	"fmt"
	"strings"
)

// ComputeWarnings checks for output style collisions and upstream provider double compression.
func ComputeWarnings(effective []ResolvedPlugin, providerIsTokenSaving bool, providerNote, providerName string) []Warning {
	var warnings []Warning

	var outputStylePlugins []string
	var tokenSavers []string

	for _, ep := range effective {
		if ep.EffectiveState != StateOn {
			continue
		}
		name := ep.Plugin.Name
		if name == "" {
			name = ep.Plugin.ID
		}

		if ep.Plugin.Category == CategoryOutputStyle {
			outputStylePlugins = append(outputStylePlugins, name)
		}
		if ep.Plugin.Category == CategoryInputCompression || ep.Plugin.Category == CategoryOutputStyle {
			tokenSavers = append(tokenSavers, name)
		}
	}

	// 1. Output style overlap
	if len(outputStylePlugins) >= 2 {
		joined := strings.Join(outputStylePlugins[:2], " and ")
		if len(outputStylePlugins) > 2 {
			joined = strings.Join(outputStylePlugins, ", ")
		}
		msg := fmt.Sprintf("%s both change answer style. Stacking them can make answers too terse and lower quality. Enable only one.", joined)
		warnings = append(warnings, Warning{
			Code:    "output_style_overlap",
			Plugins: outputStylePlugins,
			Message: msg,
		})
	}

	// 2. Upstream provider token saving overlap
	if providerIsTokenSaving && len(tokenSavers) > 0 {
		provLabel := providerName
		if provLabel == "" {
			provLabel = "Upstream provider"
		}
		notePart := ""
		if strings.TrimSpace(providerNote) != "" {
			notePart = fmt.Sprintf(" (%s)", strings.TrimSpace(providerNote))
		}
		joinedPlugins := strings.Join(tokenSavers, ", ")
		msg := fmt.Sprintf("Provider %s is marked as already applying token saving%s. %s may compress twice and remove context the model needs.", provLabel, notePart, joinedPlugins)
		warnings = append(warnings, Warning{
			Code:     "upstream_token_saving",
			Plugins:  tokenSavers,
			Provider: providerName,
			Message:  msg,
		})
	}

	return warnings
}
