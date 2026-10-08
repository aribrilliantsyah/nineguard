package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"nineguard/internal/models"
	"nineguard/internal/plugins/builtin"
)

// PipelineExecutor runs plugins in global order on a chat completion body.
type PipelineExecutor struct {
	httpClient *http.Client
}

func NewPipelineExecutor(client *http.Client) *PipelineExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &PipelineExecutor{httpClient: client}
}

// ComputeAppliedHeader returns a sorted, comma-separated union of built-in plugin IDs.
func ComputeAppliedHeader(incoming string, executedBuiltins []string) string {
	set := make(map[string]bool)
	validBuiltins := map[string]bool{
		"secretguard": true,
		"headroom":    true,
		"caveman":     true,
		"ponytail":    true,
	}

	for _, id := range strings.Split(incoming, ",") {
		id = strings.TrimSpace(id)
		if validBuiltins[id] {
			set[id] = true
		}
	}
	for _, id := range executedBuiltins {
		id = strings.TrimSpace(id)
		if validBuiltins[id] {
			set[id] = true
		}
	}

	var list []string
	for id := range set {
		list = append(list, id)
	}
	sort.Strings(list)
	return strings.Join(list, ",")
}

func parseIncomingApplied(raw string) map[string]bool {
	m := make(map[string]bool)
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			m[s] = true
		}
	}
	return m
}

func toMapSlice(raw any) ([]map[string]any, bool) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	res := make([]map[string]any, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			res = append(res, m)
		} else {
			return nil, false
		}
	}
	return res, true
}

func toAnySlice(raw []map[string]any) []any {
	res := make([]any, len(raw))
	for i, m := range raw {
		res[i] = m
	}
	return res
}

// Execute runs applicable plugins in pipeline order.
func (pe *PipelineExecutor) Execute(
	ctx context.Context,
	allPlugins []Plugin,
	allBindings []Binding,
	groups []models.ModelGroup,
	apiKeyID, apiKeyName string,
	model, providerID string,
	isBypassRequested bool,
	incomingAppliedHeader string,
	bodyBytes []byte,
) (*PipelineResult, string, error) {
	start := time.Now()

	var bodyMap map[string]any
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		return nil, "", fmt.Errorf("invalid json body: %w", err)
	}

	// 1. Resolve applicable plugins
	resolved := ResolvePluginsForRequest(allPlugins, allBindings, groups, apiKeyID, apiKeyName, model)

	// Filter to active plugins
	var active []ResolvedPlugin
	for _, r := range resolved {
		if r.EffectiveState == StateOn {
			active = append(active, r)
		}
	}

	// Sort in pipeline order ASC
	sort.Slice(active, func(i, j int) bool {
		return active[i].Plugin.PipelineOrder < active[j].Plugin.PipelineOrder
	})

	alreadyApplied := parseIncomingApplied(incomingAppliedHeader)

	var pluginsApplied []string
	var pluginsSkipped []string
	var pluginErrors []string
	var executedBuiltins []string
	totalTokensSaved := 0
	totalTokensOverhead := 0

	// Extract group names covering this model for HTTP plugin context
	var matchingGroupNames []string
	for _, g := range groups {
		for _, pat := range g.Models {
			if MatchModelPattern(pat, model) {
				matchingGroupNames = append(matchingGroupNames, g.Name)
				break
			}
		}
	}

	currentBody := bodyMap

	for _, item := range active {
		p := item.Plugin

		// Check client bypass header
		if isBypassRequested && p.Bypassable {
			pluginsSkipped = append(pluginsSkipped, p.ID+":bypassed")
			continue
		}

		// Check idempotency header: built-ins already applied downstream/upstream
		if p.Kind == KindBuiltin && alreadyApplied[p.ID] && p.Bypassable {
			pluginsSkipped = append(pluginsSkipped, p.ID+":already_applied")
			continue
		}

		timeoutMs := p.TimeoutMs
		if timeoutMs <= 0 {
			timeoutMs = 3000
		}
		pCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)

		var (
			modifiedBody   map[string]any
			tokensSaved    int
			tokensOverhead int
			skipped        bool
			skipReason     string
			rejected       bool
			rejectMsg      string
			err            error
		)

		switch p.ID {
		case "secretguard":
			msgs, ok := toMapSlice(currentBody["messages"])
			if !ok {
				err = fmt.Errorf("invalid messages format")
			} else {
				var newMsgs []map[string]any
				newMsgs, skipped, rejected, rejectMsg, err = builtin.ApplySecretGuard(msgs, item.MergedSettings)
				if err == nil && !skipped && !rejected {
					cloned := make(map[string]any, len(currentBody))
					for k, v := range currentBody {
						cloned[k] = v
					}
					cloned["messages"] = toAnySlice(newMsgs)
					modifiedBody = cloned
				}
			}
		case "headroom":
			modifiedBody, tokensSaved, skipped, skipReason, err = builtin.ApplyHeadroom(pCtx, pe.httpClient, currentBody, item.MergedSettings)
		case "caveman":
			msgs, ok := toMapSlice(currentBody["messages"])
			if !ok {
				err = fmt.Errorf("invalid messages format")
			} else {
				var newMsgs []map[string]any
				newMsgs, skipped, skipReason, tokensOverhead, err = builtin.ApplyCaveman(msgs, item.MergedSettings)
				if err == nil && !skipped {
					cloned := make(map[string]any, len(currentBody))
					for k, v := range currentBody {
						cloned[k] = v
					}
					cloned["messages"] = toAnySlice(newMsgs)
					modifiedBody = cloned
				}
			}
		case "ponytail":
			msgs, ok := toMapSlice(currentBody["messages"])
			if !ok {
				err = fmt.Errorf("invalid messages format")
			} else {
				var newMsgs []map[string]any
				newMsgs, skipped, skipReason, tokensOverhead, err = builtin.ApplyPonytail(msgs, item.MergedSettings)
				if err == nil && !skipped {
					cloned := make(map[string]any, len(currentBody))
					for k, v := range currentBody {
						cloned[k] = v
					}
					cloned["messages"] = toAnySlice(newMsgs)
					modifiedBody = cloned
				}
			}
		default:
			// HTTP Plugin
			pluginCtx := HTTPPluginContext{
				APIKeyName: apiKeyName,
				Model:      model,
				Provider:   providerID,
				Groups:     matchingGroupNames,
				Settings:   item.MergedSettings,
			}
			modifiedBody, tokensSaved, tokensOverhead, rejected, rejectMsg, err = CallHTTPPlugin(pCtx, pe.httpClient, p, currentBody, pluginCtx)
		}

		cancel()

		if rejected {
			return &PipelineResult{
				Body:           nil,
				Rejected:       true,
				RejectCode:     http.StatusForbidden,
				RejectMessage:  rejectMsg,
				DurationMs:     time.Since(start).Milliseconds(),
				PluginsApplied: pluginsApplied,
				PluginsSkipped: pluginsSkipped,
				PluginErrors:   pluginErrors,
			}, "", nil
		}

		if err != nil {
			pluginErrors = append(pluginErrors, p.ID)
			slog.Warn("plugin execution failed", "plugin", p.ID, "error", err, "policy", p.FailurePolicy)
			if p.FailurePolicy == PolicyClosed {
				return &PipelineResult{
					Body:           nil,
					Rejected:       true,
					RejectCode:     http.StatusServiceUnavailable,
					RejectMessage:  fmt.Sprintf("Plugin '%s' is unavailable.", p.ID),
					DurationMs:     time.Since(start).Milliseconds(),
					PluginsApplied: pluginsApplied,
					PluginsSkipped: pluginsSkipped,
					PluginErrors:   pluginErrors,
				}, "", nil
			}
			// Fail open: continue with unmodified body
			continue
		}

		if skipped {
			pluginsSkipped = append(pluginsSkipped, p.ID+":"+skipReason)
			continue
		}

		if modifiedBody != nil {
			currentBody = modifiedBody
		}
		pluginsApplied = append(pluginsApplied, p.ID)
		if p.Kind == KindBuiltin {
			executedBuiltins = append(executedBuiltins, p.ID)
		}
		totalTokensSaved += tokensSaved
		totalTokensOverhead += tokensOverhead
	}

	finalBytes, err := json.Marshal(currentBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to re-serialize transformed body: %w", err)
	}

	upstreamAppliedHeader := ComputeAppliedHeader(incomingAppliedHeader, executedBuiltins)

	return &PipelineResult{
		Body:           finalBytes,
		PluginsApplied: pluginsApplied,
		PluginsSkipped: pluginsSkipped,
		TokensSaved:    totalTokensSaved,
		TokensOverhead: totalTokensOverhead,
		PluginErrors:   pluginErrors,
		DurationMs:     time.Since(start).Milliseconds(),
	}, upstreamAppliedHeader, nil
}
