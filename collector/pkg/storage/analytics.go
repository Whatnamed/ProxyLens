package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// AnalyticsService 提供面向 UI 与审计的高层核算与多维聚合查询服务
type AnalyticsService struct {
	db *sql.DB
}

// NewAnalyticsService 创建 AnalyticsService 实例
func NewAnalyticsService(db *sql.DB) *AnalyticsService {
	return &AnalyticsService{db: db}
}

// GetLatestCompletedAccountingRun 获取最新已完成的核算轮次
func (a *AnalyticsService) GetLatestCompletedAccountingRun(ctx context.Context) (*AccountingRunRecord, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT
			run_id, algorithm_version, started_at, completed_at, status,
			source_journal_event_count, source_boundary_json, notes
		FROM accounting_runs
		WHERE status = 'completed'
		ORDER BY started_at DESC LIMIT 1;
	`)

	var r AccountingRunRecord
	var compStr, notes sql.NullString
	var startStr string

	err := row.Scan(
		&r.RunID, &r.AlgorithmVersion, &startStr, &compStr, &r.Status,
		&r.SourceJournalEventCount, &r.SourceBoundaryJSON, &notes,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNoCompletedAccountingRun
		}
		return nil, fmt.Errorf("failed to query latest accounting run: %w", err)
	}

	r.StartedAt, _ = time.Parse(time.RFC3339Nano, startStr)
	if compStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, compStr.String)
		r.CompletedAt = &t
	}
	r.Notes = notes.String

	return &r, nil
}

// GetUsageSummary 根据已发布的最新核算数据返回全局用量与质量摘要 (E4 / 任意范围精确计算)
func (a *AnalyticsService) GetUsageSummary(ctx context.Context, filter AnalyticsFilter) (*UsageSummary, error) {
	run, err := a.GetLatestCompletedAccountingRun(ctx)
	if err != nil {
		return nil, err
	}

	// 遍历该 run_id 下的所有 accounted_traffic 记录，执行精确时间窗口分摊计算
	rows, err := a.db.QueryContext(ctx, `
		SELECT
			observed_at, interval_start, interval_end, precision, route,
			raw_upload, raw_download, accounted_upload, accounted_download, accounting_class
		FROM accounted_traffic
		WHERE run_id = ?;
	`, run.RunID)
	if err != nil {
		return nil, fmt.Errorf("failed to query accounted traffic: %w", err)
	}
	defer rows.Close()

	var summary UsageSummary
	summary.AccountingVersion = run.AlgorithmVersion

	var qStart, qEnd *time.Time
	if filter.StartTime != nil {
		t := filter.StartTime.UTC()
		qStart = &t
	}
	if filter.EndTime != nil {
		t := filter.EndTime.UTC()
		qEnd = &t
	}

	for rows.Next() {
		var obsAtStr, prec, route, accClass string
		var intStart, intEnd sql.NullString
		var rawUp, rawDown, accUp, accDown int64

		if err := rows.Scan(
			&obsAtStr, &intStart, &intEnd, &prec, &route,
			&rawUp, &rawDown, &accUp, &accDown, &accClass,
		); err != nil {
			return nil, err
		}

		if filter.Route != "" && route != string(filter.Route) {
			continue
		}

		obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
		obsTime = obsTime.UTC()

		// 计算当前记录落入 [qStart, qEnd] 的有效比例与流量 (整型纳秒分摊，杜绝 partial hour 整桶粗暴统计)
		var effRawUp, effRawDown, effAccUp, effAccDown int64

		if prec == "exact_snapshot" || !intStart.Valid || !intEnd.Valid {
			// Exact snapshot
			inRange := true
			if qStart != nil && obsTime.Before(*qStart) {
				inRange = false
			}
			if qEnd != nil && obsTime.After(*qEnd) {
				inRange = false
			}
			if inRange {
				effRawUp = rawUp
				effRawDown = rawDown
				effAccUp = accUp
				effAccDown = accDown
			}
		} else {
			// Interval only overlap
			sTime, _ := time.Parse(time.RFC3339Nano, intStart.String)
			eTime, _ := time.Parse(time.RFC3339Nano, intEnd.String)
			sTime = sTime.UTC()
			eTime = eTime.UTC()

			clipS := sTime
			if qStart != nil && clipS.Before(*qStart) {
				clipS = *qStart
			}
			clipE := eTime
			if qEnd != nil && clipE.After(*qEnd) {
				clipE = *qEnd
			}

			if clipE.After(clipS) {
				totalNs := eTime.Sub(sTime).Nanoseconds()
				overlapNs := clipE.Sub(clipS).Nanoseconds()
				if totalNs > 0 {
					effRawUp = (rawUp * overlapNs) / totalNs
					effRawDown = (rawDown * overlapNs) / totalNs
					effAccUp = (accUp * overlapNs) / totalNs
					effAccDown = (accDown * overlapNs) / totalNs
				} else {
					effRawUp = rawUp
					effRawDown = rawDown
					effAccUp = accUp
					effAccDown = accDown
				}
			}
		}

		summary.RawObservedUpload += effRawUp
		summary.RawObservedDownload += effRawDown
		summary.UniqueObservedUpload += effAccUp
		summary.UniqueObservedDownload += effAccDown

		switch route {
		case string(types.RouteProxy):
			summary.ProxyUpload += effAccUp
			summary.ProxyDownload += effAccDown
		case string(types.RouteDirect):
			summary.DirectUpload += effAccUp
			summary.DirectDownload += effAccDown
		case string(types.RouteReject):
			summary.RejectUpload += effAccUp
			summary.RejectDownload += effAccDown
		default:
			summary.UnknownRouteUpload += effAccUp
			summary.UnknownRouteDownload += effAccDown
		}

		if accClass == string(ClassMissingAttribution) {
			summary.MissingAttributionUpload += effAccUp
			summary.MissingAttributionDownload += effAccDown
		} else if accClass == string(ClassAmbiguousRelay) {
			summary.AmbiguousRelayUpload += effAccUp
			summary.AmbiguousRelayDownload += effAccDown
		}
	}

	// 聚合 Sampling Residuals
	var resWhere []string
	var resArgs []any
	if filter.StartTime != nil {
		resWhere = append(resWhere, "observed_at >= ?")
		resArgs = append(resArgs, filter.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if filter.EndTime != nil {
		resWhere = append(resWhere, "observed_at <= ?")
		resArgs = append(resArgs, filter.EndTime.UTC().Format(time.RFC3339Nano))
	}
	resWhereSQL := ""
	if len(resWhere) > 0 {
		resWhereSQL = "WHERE " + strings.Join(resWhere, " AND ")
	}
	_ = a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(residual_upload), 0), COALESCE(SUM(residual_download), 0)
		FROM residual_intervals %s;
	`, resWhereSQL), resArgs...).Scan(&summary.SamplingResidualUpload, &summary.SamplingResidualDownload)

	// 聚合 Controller Gap 物理流量
	var gapWhere = []string{"source = 'controller_stream'"}
	var gapArgs []any
	if filter.StartTime != nil {
		gapWhere = append(gapWhere, "(ended_at >= ? OR ended_at IS NULL)")
		gapArgs = append(gapArgs, filter.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if filter.EndTime != nil {
		gapWhere = append(gapWhere, "started_at <= ?")
		gapArgs = append(gapArgs, filter.EndTime.UTC().Format(time.RFC3339Nano))
	}
	gapWhereSQL := "WHERE " + strings.Join(gapWhere, " AND ")
	_ = a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(global_gap_upload_delta), 0), COALESCE(SUM(global_gap_download_delta), 0)
		FROM monitoring_gaps %s;
	`, gapWhereSQL), gapArgs...).Scan(&summary.ControllerGapPhysicalUpload, &summary.ControllerGapPhysicalDownload)

	// 计算 Coverage
	cov, err := a.GetCoverage(ctx, filter.StartTime, filter.EndTime)
	if err == nil {
		summary.Coverage = cov
	}

	return &summary, nil
}

// GetTopDimensions 通用多维聚合排行查询 (全窗口 Distinct 连接数与精确时间分摊)
func (a *AnalyticsService) GetTopDimensions(ctx context.Context, dimType string, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	run, err := a.GetLatestCompletedAccountingRun(ctx)
	if err != nil {
		return nil, err
	}

	// 构造多维字段投影
	dimColumn := "process"
	switch dimType {
	case "process":
		dimColumn = "process"
	case "host":
		dimColumn = "host"
	case "destination_ip":
		dimColumn = "destination_ip"
	case "rule":
		dimColumn = "rule"
	case "rule_payload":
		dimColumn = "rule_payload"
	case "final_proxy":
		dimColumn = "final_proxy"
	case "top_policy_group":
		dimColumn = "top_policy_group"
	case "network":
		dimColumn = "network"
	}

	rows, err := a.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			session_id, epoch_id, connection_id, observed_at,
			interval_start, interval_end, precision, route,
			accounted_upload, accounted_download,
			COALESCE(%s, '') AS dim_key
		FROM accounted_traffic
		WHERE run_id = ? AND %s IS NOT NULL AND %s != '';
	`, dimColumn, dimColumn, dimColumn), run.RunID)
	if err != nil {
		return nil, fmt.Errorf("failed to query top dimensions: %w", err)
	}
	defer rows.Close()

	type itemKey struct {
		key   string
		route types.RouteType
	}
	type itemAgg struct {
		up, down               int64
		exactUp, exactDown     int64
		estUp, estDown         int64
		distinctConns          map[string]bool
	}

	aggMap := make(map[itemKey]*itemAgg)

	var qStart, qEnd *time.Time
	if filter.StartTime != nil {
		t := filter.StartTime.UTC()
		qStart = &t
	}
	if filter.EndTime != nil {
		t := filter.EndTime.UTC()
		qEnd = &t
	}

	for rows.Next() {
		var sessID, connID, obsAtStr, prec, route, dimKey string
		var epochID int
		var intStart, intEnd sql.NullString
		var accUp, accDown int64

		if err := rows.Scan(
			&sessID, &epochID, &connID, &obsAtStr,
			&intStart, &intEnd, &prec, &route,
			&accUp, &accDown, &dimKey,
		); err != nil {
			return nil, err
		}

		if filter.Route != "" && route != string(filter.Route) {
			continue
		}

		obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
		obsTime = obsTime.UTC()

		var effUp, effDown int64
		var isExact bool

		if prec == "exact_snapshot" || !intStart.Valid || !intEnd.Valid {
			inRange := true
			if qStart != nil && obsTime.Before(*qStart) {
				inRange = false
			}
			if qEnd != nil && obsTime.After(*qEnd) {
				inRange = false
			}
			if inRange {
				effUp = accUp
				effDown = accDown
				isExact = true
			}
		} else {
			sTime, _ := time.Parse(time.RFC3339Nano, intStart.String)
			eTime, _ := time.Parse(time.RFC3339Nano, intEnd.String)
			sTime = sTime.UTC()
			eTime = eTime.UTC()

			clipS := sTime
			if qStart != nil && clipS.Before(*qStart) {
				clipS = *qStart
			}
			clipE := eTime
			if qEnd != nil && clipE.After(*qEnd) {
				clipE = *qEnd
			}

			if clipE.After(clipS) {
				totalNs := eTime.Sub(sTime).Nanoseconds()
				overlapNs := clipE.Sub(clipS).Nanoseconds()
				if totalNs > 0 {
					effUp = (accUp * overlapNs) / totalNs
					effDown = (accDown * overlapNs) / totalNs
				} else {
					effUp = accUp
					effDown = accDown
				}
				isExact = false
			}
		}

		if effUp == 0 && effDown == 0 {
			continue
		}

		k := itemKey{key: dimKey, route: types.RouteType(route)}
		agg, ok := aggMap[k]
		if !ok {
			agg = &itemAgg{distinctConns: make(map[string]bool)}
			aggMap[k] = agg
		}

		agg.up += effUp
		agg.down += effDown
		if isExact {
			agg.exactUp += effUp
			agg.exactDown += effDown
		} else {
			agg.estUp += effUp
			agg.estDown += effDown
		}
		connFullKey := fmt.Sprintf("%s:%d:%s", sessID, epochID, connID)
		agg.distinctConns[connFullKey] = true
	}

	var results []TopDimensionItem
	for k, v := range aggMap {
		results = append(results, TopDimensionItem{
			Key:                    k.key,
			Route:                  k.route,
			UploadBytes:            v.up,
			DownloadBytes:          v.down,
			TotalBytes:             v.up + v.down,
			ConnectionCount:        int64(len(v.distinctConns)), // 全窗口 Distinct 连接数
			ExactUploadBytes:       v.exactUp,
			ExactDownloadBytes:     v.exactDown,
			EstimatedUploadBytes:   v.estUp,
			EstimatedDownloadBytes: v.estDown,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalBytes > results[j].TotalBytes
	})

	limit := 10
	if filter.Limit > 0 {
		limit = filter.Limit
	}
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetTopProcesses 查询进程消耗排行
func (a *AnalyticsService) GetTopProcesses(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "process", filter)
}

// GetTopHosts 查询域名/目标排行
func (a *AnalyticsService) GetTopHosts(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "host", filter)
}

// GetTopRules 查询分流规则排行
func (a *AnalyticsService) GetTopRules(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "rule", filter)
}

// GetTopFinalProxies 查询最终出站节点排行 (来自历史 chains[0])
func (a *AnalyticsService) GetTopFinalProxies(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "final_proxy", filter)
}

// GetProtocolBreakdown 查询网络协议分布 (TCP / UDP)
func (a *AnalyticsService) GetProtocolBreakdown(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "network", filter)
}

// GetCoverage 计算指定时间窗口内的监控覆盖度与缺口并集 (支持 Trailing Offline & Future Clip & Provenance)
func (a *AnalyticsService) GetCoverage(ctx context.Context, start, end *time.Time) (*CoverageSummary, error) {
	// 1. 查找已知监控起始时间 (第一条 session 的 started_at)
	var firstSessStartStr sql.NullString
	err := a.db.QueryRowContext(ctx, "SELECT MIN(started_at) FROM collector_sessions;").Scan(&firstSessStartStr)
	if err != nil || !firstSessStartStr.Valid || firstSessStartStr.String == "" {
		return &CoverageSummary{
			RequestedStart: start,
			RequestedEnd:   end,
		}, nil
	}

	knownScopeStart, _ := time.Parse(time.RFC3339Nano, firstSessStartStr.String)
	knownScopeStart = knownScopeStart.UTC()

	now := time.Now().UTC()
	reqStart := knownScopeStart
	if start != nil {
		reqStart = start.UTC()
	}
	reqEnd := now
	if end != nil {
		reqEnd = end.UTC()
	}

	if !reqEnd.After(reqStart) {
		reqEnd = reqStart.Add(time.Second)
	}

	summary := &CoverageSummary{
		RequestedStart:  &reqStart,
		RequestedEnd:    &reqEnd,
		KnownScopeStart: &knownScopeStart,
	}

	// 2. Future Query Clip: 如果请求完全在未来
	if reqStart.After(now) {
		summary.FutureDurationMs = reqEnd.Sub(reqStart).Milliseconds()
		summary.CoverageRatio = nil
		return summary, nil
	}

	// 如果请求终点超出当前时间，截断到 now 并记录 FutureDuration
	effEnd := reqEnd
	if effEnd.After(now) {
		summary.FutureDurationMs = effEnd.Sub(now).Milliseconds()
		effEnd = now
	}

	// 检查 requested 是否完全早于 known scope
	if effEnd.Before(knownScopeStart) || effEnd.Equal(knownScopeStart) {
		summary.OutsideKnownScopeMs = effEnd.Sub(reqStart).Milliseconds()
		summary.CoverageRatio = nil
		return summary, nil
	}

	effStart := reqStart
	if effStart.Before(knownScopeStart) {
		summary.OutsideKnownScopeMs = knownScopeStart.Sub(effStart).Milliseconds()
		effStart = knownScopeStart
	}

	summary.EffectiveScopeStart = &effStart
	summary.EffectiveScopeEnd = &effEnd
	effectiveWindowMs := effEnd.Sub(effStart).Milliseconds()
	if effectiveWindowMs <= 0 {
		effectiveWindowMs = 1
	}

	// 3. 读取所有与 [effStart, effEnd] 重叠的数据库记录 Gaps
	rows, err := a.db.QueryContext(ctx, `
		SELECT source, started_at, ended_at, reason
		FROM monitoring_gaps
		WHERE (ended_at >= ? OR ended_at IS NULL) AND started_at <= ?;
	`, effStart.Format(time.RFC3339Nano), effEnd.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("failed to query gaps for coverage: %w", err)
	}
	defer rows.Close()

	type rawInterval struct {
		start, end     time.Time
		source, reason string
	}
	var rawIntervals []rawInterval
	var ctrlGapMs, offGapMs int64

	for rows.Next() {
		var src, startStr, reason string
		var endStr sql.NullString
		if err := rows.Scan(&src, &startStr, &endStr, &reason); err != nil {
			return nil, err
		}
		gStart, _ := time.Parse(time.RFC3339Nano, startStr)
		gStart = gStart.UTC()
		gEnd := now
		if endStr.Valid && endStr.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, endStr.String)
			gEnd = t.UTC()
		}

		clipStart := gStart
		if clipStart.Before(effStart) {
			clipStart = effStart
		}
		clipEnd := gEnd
		if clipEnd.After(effEnd) {
			clipEnd = effEnd
		}

		if clipEnd.After(clipStart) {
			dur := clipEnd.Sub(clipStart).Milliseconds()
			if src == "controller_stream" {
				ctrlGapMs += dur
			} else {
				offGapMs += dur
			}
			rawIntervals = append(rawIntervals, rawInterval{
				start: clipStart, end: clipEnd, source: src, reason: reason,
			})
		}
	}

	// 4. Trailing Offline Gap: 检查最新 session 是否处于停止/中断状态
	var latestStatus, latestEndedStr, latestLastEventStr sql.NullString
	err = a.db.QueryRowContext(ctx, `
		SELECT status, ended_at, last_event_at
		FROM collector_sessions
		ORDER BY started_at DESC LIMIT 1;
	`).Scan(&latestStatus, &latestEndedStr, &latestLastEventStr)
	if err == nil && latestStatus.Valid {
		if latestStatus.String == string(SessionStatusClosedClean) || latestStatus.String == string(SessionStatusInterrupted) {
			trailStartStr := latestEndedStr.String
			if trailStartStr == "" {
				trailStartStr = latestLastEventStr.String
			}
			if trailStartStr != "" {
				tEnd, _ := time.Parse(time.RFC3339Nano, trailStartStr)
				tEnd = tEnd.UTC()
				if tEnd.Before(effEnd) {
					tClipStart := tEnd
					if tClipStart.Before(effStart) {
						tClipStart = effStart
					}
					if effEnd.After(tClipStart) {
						offDur := effEnd.Sub(tClipStart).Milliseconds()
						offGapMs += offDur
						rawIntervals = append(rawIntervals, rawInterval{
							start: tClipStart, end: effEnd,
							source: "collector_session_boundary",
							reason: "collector_stopped_or_offline_trailing",
						})
					}
				}
			}
		}
	}

	summary.ControllerGapDurationMs = ctrlGapMs
	summary.CollectorOfflineDurationMs = offGapMs

	// 5. 执行区间求并集 (Interval Union，保留 Provenance)
	sort.Slice(rawIntervals, func(i, j int) bool {
		return rawIntervals[i].start.Before(rawIntervals[j].start)
	})

	var merged []MergedGap
	for _, it := range rawIntervals {
		if len(merged) == 0 {
			merged = append(merged, MergedGap{
				Source:     it.source,
				Sources:    []string{it.source},
				StartedAt:  it.start,
				EndedAt:    it.end,
				DurationMs: it.end.Sub(it.start).Milliseconds(),
				Reason:     it.reason,
				Reasons:    []string{it.reason},
			})
			continue
		}

		last := &merged[len(merged)-1]
		if !it.start.After(last.EndedAt) {
			// 重叠合并，同时保留 provenance 列表
			if it.end.After(last.EndedAt) {
				last.EndedAt = it.end
				last.DurationMs = last.EndedAt.Sub(last.StartedAt).Milliseconds()
			}
			last.Sources = append(last.Sources, it.source)
			last.Reasons = append(last.Reasons, it.reason)
		} else {
			merged = append(merged, MergedGap{
				Source:     it.source,
				Sources:    []string{it.source},
				StartedAt:  it.start,
				EndedAt:    it.end,
				DurationMs: it.end.Sub(it.start).Milliseconds(),
				Reason:     it.reason,
				Reasons:    []string{it.reason},
			})
		}
	}

	summary.MergedGaps = merged

	// 6. 计算已覆盖与未覆盖时间
	var uncoveredMs int64
	for _, m := range merged {
		uncoveredMs += m.DurationMs
	}
	if uncoveredMs > effectiveWindowMs {
		uncoveredMs = effectiveWindowMs
	}
	coveredMs := effectiveWindowMs - uncoveredMs

	summary.UncoveredDurationMs = uncoveredMs
	summary.CoveredDurationMs = coveredMs
	ratio := float64(coveredMs) / float64(effectiveWindowMs)
	summary.CoverageRatio = &ratio

	return summary, nil
}
