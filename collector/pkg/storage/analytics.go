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

// GetUsageSummary 根据已发布的最新核算数据返回全局用量与质量摘要 (E4)
func (a *AnalyticsService) GetUsageSummary(ctx context.Context, filter AnalyticsFilter) (*UsageSummary, error) {
	run, err := a.GetLatestCompletedAccountingRun(ctx)
	if err != nil {
		return nil, err
	}

	var whereClauses = []string{"run_id = ?"}
	var args = []any{run.RunID}

	if filter.StartTime != nil {
		whereClauses = append(whereClauses, "observed_at >= ?")
		args = append(args, filter.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if filter.EndTime != nil {
		whereClauses = append(whereClauses, "observed_at <= ?")
		args = append(args, filter.EndTime.UTC().Format(time.RFC3339Nano))
	}
	if filter.Route != "" {
		whereClauses = append(whereClauses, "route = ?")
		args = append(args, string(filter.Route))
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	querySQL := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(raw_upload), 0),
			COALESCE(SUM(raw_download), 0),
			COALESCE(SUM(accounted_upload), 0),
			COALESCE(SUM(accounted_download), 0),

			COALESCE(SUM(CASE WHEN route = 'PROXY' THEN accounted_upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN route = 'PROXY' THEN accounted_download ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN route = 'DIRECT' THEN accounted_upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN route = 'DIRECT' THEN accounted_download ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN route = 'REJECT' THEN accounted_upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN route = 'REJECT' THEN accounted_download ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN route = 'UNKNOWN' THEN accounted_upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN route = 'UNKNOWN' THEN accounted_download ELSE 0 END), 0),

			COALESCE(SUM(CASE WHEN accounting_class = 'missing_attribution' THEN accounted_upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN accounting_class = 'missing_attribution' THEN accounted_download ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN accounting_class = 'ambiguous_relay' THEN accounted_upload ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN accounting_class = 'ambiguous_relay' THEN accounted_download ELSE 0 END), 0)
		FROM accounted_traffic
		%s;
	`, whereSQL)

	var summary UsageSummary
	summary.AccountingVersion = run.AlgorithmVersion

	err = a.db.QueryRowContext(ctx, querySQL, args...).Scan(
		&summary.RawObservedUpload, &summary.RawObservedDownload,
		&summary.UniqueObservedUpload, &summary.UniqueObservedDownload,
		&summary.ProxyUpload, &summary.ProxyDownload,
		&summary.DirectUpload, &summary.DirectDownload,
		&summary.RejectUpload, &summary.RejectDownload,
		&summary.UnknownRouteUpload, &summary.UnknownRouteDownload,
		&summary.MissingAttributionUpload, &summary.MissingAttributionDownload,
		&summary.AmbiguousRelayUpload, &summary.AmbiguousRelayDownload,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage summary: %w", err)
	}

	// 聚合 Sampling Residuals (E4.2: 独立诊断量，不伪装成 Unknown Traffic)
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

	// 聚合 Controller Gap 物理流量 (E4.3)
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

// GetTopDimensions 通用多维聚合排行查询 (E7.3)
func (a *AnalyticsService) GetTopDimensions(ctx context.Context, dimType string, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	run, err := a.GetLatestCompletedAccountingRun(ctx)
	if err != nil {
		return nil, err
	}

	var whereClauses = []string{"run_id = ?", "dimension_type = ?"}
	var args = []any{run.RunID, dimType}

	if filter.StartTime != nil {
		whereClauses = append(whereClauses, "bucket_start >= ?")
		args = append(args, filter.StartTime.UTC().Truncate(time.Hour).Format(time.RFC3339Nano))
	}
	if filter.EndTime != nil {
		whereClauses = append(whereClauses, "bucket_start <= ?")
		args = append(args, filter.EndTime.UTC().Truncate(time.Hour).Format(time.RFC3339Nano))
	}
	if filter.Route != "" {
		whereClauses = append(whereClauses, "route = ?")
		args = append(args, string(filter.Route))
	}

	limit := 10
	if filter.Limit > 0 {
		limit = filter.Limit
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	querySQL := fmt.Sprintf(`
		SELECT
			dimension_key,
			route,
			SUM(upload_bytes) AS up,
			SUM(download_bytes) AS down,
			SUM(upload_bytes + download_bytes) AS total,
			SUM(connection_count) AS conns,
			SUM(exact_upload_bytes) AS ex_up,
			SUM(exact_download_bytes) AS ex_down,
			SUM(estimated_upload_bytes) AS est_up,
			SUM(estimated_download_bytes) AS est_down
		FROM usage_hourly_dimensions
		%s
		GROUP BY dimension_key, route
		ORDER BY total DESC
		LIMIT %d;
	`, whereSQL, limit)

	rows, err := a.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query top %s: %w", dimType, err)
	}
	defer rows.Close()

	var items []TopDimensionItem
	for rows.Next() {
		var item TopDimensionItem
		var routeStr string
		if err := rows.Scan(
			&item.Key, &routeStr, &item.UploadBytes, &item.DownloadBytes, &item.TotalBytes,
			&item.ConnectionCount, &item.ExactUploadBytes, &item.ExactDownloadBytes,
			&item.EstimatedUploadBytes, &item.EstimatedDownloadBytes,
		); err != nil {
			return nil, err
		}
		item.Route = types.RouteType(routeStr)
		items = append(items, item)
	}

	return items, nil
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

// GetTopFinalProxies 查询最终出站节点排行 (E7.3: 来自历史 chains[0]，不读取当前 selector 状态)
func (a *AnalyticsService) GetTopFinalProxies(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "final_proxy", filter)
}

// GetProtocolBreakdown 查询网络协议分布 (TCP / UDP)
func (a *AnalyticsService) GetProtocolBreakdown(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "network", filter)
}

// GetCoverage 计算指定时间窗口内的监控覆盖度与缺口并集 (E6)
func (a *AnalyticsService) GetCoverage(ctx context.Context, start, end *time.Time) (*CoverageSummary, error) {
	// 1. 查找已知监控的起始时间 (第一条 session 的 started_at)
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

	// 2. 检查 requested 是否完全早于 known scope
	if reqEnd.Before(knownScopeStart) || reqEnd.Equal(knownScopeStart) {
		summary.OutsideKnownScopeMs = reqEnd.Sub(reqStart).Milliseconds()
		summary.CoverageRatio = nil // 完全在 known scope 之前返回 nil (E6.3)
		return summary, nil
	}

	// 计算有效窗口
	effStart := reqStart
	if effStart.Before(knownScopeStart) {
		summary.OutsideKnownScopeMs = knownScopeStart.Sub(effStart).Milliseconds()
		effStart = knownScopeStart
	}
	effEnd := reqEnd

	summary.EffectiveScopeStart = &effStart
	summary.EffectiveScopeEnd = &effEnd
	effectiveWindowMs := effEnd.Sub(effStart).Milliseconds()
	if effectiveWindowMs <= 0 {
		effectiveWindowMs = 1
	}

	// 3. 读取所有与 [effStart, effEnd] 重叠的 gaps
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
		start, end time.Time
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

		// 裁剪到 [effStart, effEnd]
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

	summary.ControllerGapDurationMs = ctrlGapMs
	summary.CollectorOfflineDurationMs = offGapMs

	// 4. 执行区间求并集 (Interval Union, E6.2)
	sort.Slice(rawIntervals, func(i, j int) bool {
		return rawIntervals[i].start.Before(rawIntervals[j].start)
	})

	var merged []MergedGap
	for _, it := range rawIntervals {
		if len(merged) == 0 {
			merged = append(merged, MergedGap{
				Source: it.source, StartedAt: it.start, EndedAt: it.end,
				DurationMs: it.end.Sub(it.start).Milliseconds(), Reason: it.reason,
			})
			continue
		}

		last := &merged[len(merged)-1]
		if !it.start.After(last.EndedAt) {
			// 重叠，合并
			if it.end.After(last.EndedAt) {
				last.EndedAt = it.end
				last.DurationMs = last.EndedAt.Sub(last.StartedAt).Milliseconds()
			}
		} else {
			merged = append(merged, MergedGap{
				Source: it.source, StartedAt: it.start, EndedAt: it.end,
				DurationMs: it.end.Sub(it.start).Milliseconds(), Reason: it.reason,
			})
		}
	}

	summary.MergedGaps = merged

	// 5. 计算未覆盖与已覆盖时间
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
