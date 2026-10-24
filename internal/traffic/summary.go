package traffic

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"nineguard/internal/pricing"
	"nineguard/internal/timeutil"
)

// GetUsageSummary computes the complete usage telemetry summary for an API key per Spec §6.
func (m *Manager) GetUsageSummary(keyID string, rng string, loc *time.Location, now time.Time, pricingMgr *pricing.Manager) (*UsageSummary, error) {
	if loc == nil {
		loc = time.UTC
	}
	rng = strings.ToLower(strings.TrimSpace(rng))
	if rng == "" {
		rng = "30d"
	}
	if rng != "7d" && rng != "30d" && rng != "all" {
		return nil, fmt.Errorf("invalid range: %q, must be 7d, 30d, or all", rng)
	}

	nowLoc := now.In(loc)
	todayStart := time.Date(nowLoc.Year(), nowLoc.Month(), nowLoc.Day(), 0, 0, 0, 0, loc)
	todayEnd := time.Date(nowLoc.Year(), nowLoc.Month(), nowLoc.Day(), 23, 59, 59, 999999999, loc)

	var startDate time.Time
	var endDate = todayEnd
	var heatmapStartDate time.Time

	switch rng {
	case "7d":
		startDate = todayStart.AddDate(0, 0, -6)
		heatmapStartDate = startDate
	case "30d":
		startDate = todayStart.AddDate(0, 0, -29)
		heatmapStartDate = startDate
	case "all":
		// Capped at last 371 days (53 weeks) for daily series per Spec §6.1
		heatmapStartDate = todayStart.AddDate(0, 0, -370)
	}

	offsetMod := timeutil.SQLiteOffsetModifier(loc, nowLoc)

	// Build filter conditions
	var conditions []string
	var args []any

	conditions = append(conditions, "(api_key_id = ? OR api_key = ?)")
	args = append(args, keyID, keyID)

	conditions = append(conditions, "model != 'nineguard/system'")

	if !startDate.IsZero() {
		conditions = append(conditions, "timestamp >= datetime(?)")
		args = append(args, startDate.UTC().Format("2006-01-02 15:04:05"))
	}
	conditions = append(conditions, "timestamp <= datetime(?)")
	args = append(args, endDate.UTC().Format("2006-01-02 15:04:05"))

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	summary := &UsageSummary{
		Range:    rng,
		Timezone: loc.String(),
		From:     startDate.Format("2006-01-02"),
		To:       todayEnd.Format("2006-01-02"),
	}
	if rng == "all" {
		summary.From = ""
	}

	// 1. Overall Aggregates Query
	aggQuery := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN total_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN prompt_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN completion_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN cache_read_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN cache_write_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN reasoning_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 AND usage_estimated = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN cost_usd ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 AND cost_usd IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN thinking_requested = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN thinking_delivered = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN thinking_requested = 1 AND thinking_delivered = 0 AND NOT (api_format = 'chat' AND LOWER(model) LIKE '%%claude%%') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN model_fallback = 1 THEN 1 ELSE 0 END), 0),
			MIN(timestamp)
		FROM traffic_logs
		%s
	`, whereClause)

	var totalReqs, okReqs, errReqs int64
	var promptToks, compToks, totalToks, cacheRead, cacheWrite, reasoningToks int64
	var estReqs, unpricedReqs int64
	var costUSD float64
	var thinkReq, thinkDel, thinkLost, fallbacks int64
	var minTs sql.NullString

	err := m.db.QueryRow(aggQuery, args...).Scan(
		&totalReqs, &okReqs, &errReqs,
		&totalToks, &promptToks, &compToks,
		&cacheRead, &cacheWrite, &reasoningToks,
		&estReqs, &costUSD, &unpricedReqs,
		&thinkReq, &thinkDel, &thinkLost,
		&fallbacks, &minTs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage summary aggregates: %w", err)
	}

	summary.Requests = okReqs
	summary.Errors = errReqs
	if totalReqs > 0 {
		summary.ErrorRatePct = math.Round(float64(errReqs)/float64(totalReqs)*1000) / 10
	}
	summary.Tokens = TokensBreakdown{
		Prompt:     promptToks,
		Completion: compToks,
		Total:      totalToks,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Reasoning:  reasoningToks,
	}
	summary.EstimatedRequests = estReqs
	if promptToks > 0 {
		summary.CacheHitPct = math.Round(float64(cacheRead)/float64(promptToks)*1000) / 10
	}
	summary.Thinking = ThinkingBreakdown{
		Requested: thinkReq,
		Delivered: thinkDel,
		Lost:      thinkLost,
	}
	summary.Fallbacks = fallbacks

	if rng == "all" && minTs.Valid && len(minTs.String) >= 10 {
		summary.From = minTs.String[:10]
	}

	// 2. Inferred Sessions Query
	gapMins := m.GetSessionGapMinutes()
	if gapMins < 1 {
		gapMins = 30
	}
	gapSecs := gapMins * 60

	sessionQuery := fmt.Sprintf(`
		WITH ordered AS (
			SELECT 
				timestamp,
				LAG(timestamp) OVER (ORDER BY timestamp ASC) AS prev_ts
			FROM traffic_logs
			%s
		)
		SELECT COUNT(*) FROM ordered
		WHERE prev_ts IS NULL 
		   OR (strftime('%%s', timestamp) - strftime('%%s', prev_ts)) > ?
	`, whereClause)

	sessionArgs := append(append([]any{}, args...), gapSecs)
	if totalReqs > 0 {
		_ = m.db.QueryRow(sessionQuery, sessionArgs...).Scan(&summary.Sessions)
	}

	// 3. Active Days Query
	activeDaysQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT strftime('%%Y-%%m-%%d', datetime(timestamp, ?)))
		FROM traffic_logs
		%s
	`, whereClause)
	activeDaysArgs := append([]any{offsetMod}, args...)
	_ = m.db.QueryRow(activeDaysQuery, activeDaysArgs...).Scan(&summary.ActiveDays)

	// 4. Peak Hour Query
	peakHourQuery := fmt.Sprintf(`
		SELECT strftime('%%H', datetime(timestamp, ?)) AS hr, COUNT(*) AS cnt
		FROM traffic_logs
		%s
		GROUP BY hr
		ORDER BY cnt DESC, hr ASC
		LIMIT 1
	`, whereClause)
	var peakHrStr string
	var peakCnt int64
	if err := m.db.QueryRow(peakHourQuery, activeDaysArgs...).Scan(&peakHrStr, &peakCnt); err == nil && peakHrStr != "" {
		var hrInt int
		_, _ = fmt.Sscanf(peakHrStr, "%d", &hrInt)
		summary.PeakHour = hrInt
		summary.PeakHourFormatted = fmt.Sprintf("%02d:00", hrInt)
	} else {
		summary.PeakHour = 0
		summary.PeakHourFormatted = "00:00"
	}

	// 5. Favorite Model Query
	favModelQuery := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(model_id, ''), model) AS mdl, SUM(total_tokens) AS toks
		FROM traffic_logs
		%s AND status_code < 400
		GROUP BY mdl
		ORDER BY toks DESC
		LIMIT 1
	`, whereClause)
	var favMdl string
	var favToks int64
	if err := m.db.QueryRow(favModelQuery, args...).Scan(&favMdl, &favToks); err == nil {
		summary.FavoriteModel = favMdl
	}

	// 6. Cost Calculation
	if pricingMgr != nil {
		costSettings, _ := pricingMgr.GetCostSettings()
		rate := costSettings.EffectiveRate
		if rate <= 0 {
			rate = 1.0
		}
		cur := costSettings.EffectiveCurrency
		if cur == "" {
			cur = "USD"
		}
		displayCost := math.Round(costUSD*rate*100) / 100
		summary.Cost = &CostBreakdown{
			USD:              costUSD,
			Display:          displayCost,
			DisplayFormatted: pricing.FormatCost(costUSD, cur, rate, unpricedReqs),
			Currency:         cur,
			Rate:             rate,
			UnpricedRequests: unpricedReqs,
		}
	}

	// 7. Comparison Against Literature
	if m.IsUsageComparisonEnabled() && totalToks > 0 {
		summary.Comparison = CompareTokens(totalToks)
	}

	// 8. Daily Heatmap / Daily Usage Query
	// Generate local day buckets from heatmapStartDate until todayEnd
	dailyMap := make(map[string]*DailyUsagePoint)
	var dailyDates []string
	if !heatmapStartDate.IsZero() {
		for d := heatmapStartDate; !d.After(todayEnd); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			dailyDates = append(dailyDates, ds)
			dailyMap[ds] = &DailyUsagePoint{Date: ds}
		}
	}

	dailyQuery := fmt.Sprintf(`
		SELECT
			strftime('%%Y-%%m-%%d', datetime(timestamp, ?)) AS d,
			COALESCE(SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status_code < 400 THEN total_tokens ELSE 0 END), 0),
			SUM(CASE WHEN status_code < 400 THEN cost_usd ELSE NULL END)
		FROM traffic_logs
		%s
		GROUP BY d
		ORDER BY d ASC
	`, whereClause)

	dRows, err := m.db.Query(dailyQuery, activeDaysArgs...)
	if err == nil {
		for dRows.Next() {
			var d string
			var reqs, toks int64
			var dCost *float64
			if err := dRows.Scan(&d, &reqs, &toks, &dCost); err == nil {
				if pt, ok := dailyMap[d]; ok {
					pt.Requests = reqs
					pt.Tokens = toks
					pt.Cost = dCost
				} else {
					dailyDates = append(dailyDates, d)
					dailyMap[d] = &DailyUsagePoint{
						Date:     d,
						Requests: reqs,
						Tokens:   toks,
						Cost:     dCost,
					}
				}
			}
		}
		dRows.Close()
	}

	for _, ds := range dailyDates {
		if pt, ok := dailyMap[ds]; ok {
			summary.Daily = append(summary.Daily, *pt)
		}
	}

	// 9. Models Breakdown Query (Top 5 models by total_tokens + other)
	modelsQuery := fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(model_id, ''), model) AS mdl,
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0),
			COALESCE(SUM(total_tokens), 0),
			SUM(cost_usd)
		FROM traffic_logs
		%s AND status_code < 400
		GROUP BY mdl
		ORDER BY SUM(total_tokens) DESC
	`, whereClause)

	mRows, err := m.db.Query(modelsQuery, args...)
	if err == nil {
		var rank int
		var otherPoint *ModelUsagePoint
		for mRows.Next() {
			var mdl string
			var pToks, cToks, tToks int64
			var mCost *float64
			if err := mRows.Scan(&mdl, &pToks, &cToks, &tToks, &mCost); err == nil {
				pct := 0.0
				if totalToks > 0 {
					pct = math.Round(float64(tToks)/float64(totalToks)*1000) / 10
				}
				rank++
				if rank <= 5 {
					summary.Models = append(summary.Models, ModelUsagePoint{
						ModelID:    mdl,
						Prompt:     pToks,
						Completion: cToks,
						Total:      tToks,
						Cost:       mCost,
						PctTokens:  pct,
						Daily:      []ModelDailyUsage{},
					})
				} else {
					if otherPoint == nil {
						otherPoint = &ModelUsagePoint{
							ModelID: "other",
							Daily:   []ModelDailyUsage{},
						}
					}
					otherPoint.Prompt += pToks
					otherPoint.Completion += cToks
					otherPoint.Total += tToks
					if mCost != nil {
						if otherPoint.Cost == nil {
							c := *mCost
							otherPoint.Cost = &c
						} else {
							*otherPoint.Cost += *mCost
						}
					}
					otherPoint.PctTokens += pct
				}
			}
		}
		mRows.Close()
		if otherPoint != nil {
			otherPoint.PctTokens = math.Round(otherPoint.PctTokens*10) / 10
			summary.Models = append(summary.Models, *otherPoint)
		}
	}

	// Fill daily breakdown per top model
	if len(summary.Models) > 0 {
		topModelSet := make(map[string]int)
		for i, mod := range summary.Models {
			if mod.ModelID != "other" {
				topModelSet[strings.ToLower(mod.ModelID)] = i
			}
		}

		modelDailyQuery := fmt.Sprintf(`
			SELECT
				COALESCE(NULLIF(model_id, ''), model) AS mdl,
				strftime('%%Y-%%m-%%d', datetime(timestamp, ?)) AS d,
				COALESCE(SUM(total_tokens), 0)
			FROM traffic_logs
			%s AND status_code < 400
			GROUP BY mdl, d
			ORDER BY d ASC
		`, whereClause)

		mdRows, err := m.db.Query(modelDailyQuery, activeDaysArgs...)
		if err == nil {
			for mdRows.Next() {
				var mdl, d string
				var tot int64
				if err := mdRows.Scan(&mdl, &d, &tot); err == nil {
					idx, ok := topModelSet[strings.ToLower(mdl)]
					if ok {
						summary.Models[idx].Daily = append(summary.Models[idx].Daily, ModelDailyUsage{
							Date:  d,
							Total: tot,
						})
					} else {
						// add to 'other' model daily bucket if present
						for i := range summary.Models {
							if summary.Models[i].ModelID == "other" {
								summary.Models[i].Daily = append(summary.Models[i].Daily, ModelDailyUsage{
									Date:  d,
									Total: tot,
								})
								break
							}
						}
					}
				}
			}
			mdRows.Close()
		}
	}

	return summary, nil
}
