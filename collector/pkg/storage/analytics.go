package storage

import (
	"context"
	"database/sql"
	"errors"
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

// accountingScope resolves which derived accounting source queries should read:
// the active incremental accounting v2 generation when one exists, otherwise
// the latest completed legacy accounting run (Phase 3S fallback contract).
type accountingScope struct {
	useV2            bool
	runID            string
	generationID     string
	algorithmVersion string
}

// accountedTable returns the derived traffic table and key predicate for the scope.
func (s *accountingScope) accountedTable() (table string, keyCol string, keyVal string) {
	if s.useV2 {
		return "accounted_traffic_v2", "generation_id", s.generationID
	}
	return "accounted_traffic", "run_id", s.runID
}

// hourlyDimensionsTable returns the hourly materialization that belongs to the
// same authority as accountedTable. Temporal intelligence must not resolve a
// second, independent accounting source.
func (s *accountingScope) hourlyDimensionsTable() (table string, keyCol string, keyVal string) {
	if s.useV2 {
		return "usage_hourly_dimensions_v2", "generation_id", s.generationID
	}
	return "usage_hourly_dimensions", "run_id", s.runID
}

func (a *AnalyticsService) resolveAccountingScope(ctx context.Context) (*accountingScope, error) {
	gen, err := GetActiveAccountingGeneration(ctx, a.db)
	if err != nil && !errors.Is(err, ErrNoActiveGeneration) && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if gen != nil {
		return &accountingScope{
			useV2:            true,
			generationID:     gen.GenerationID,
			runID:            gen.GenerationID,
			algorithmVersion: gen.AlgorithmVersion,
		}, nil
	}
	run, err := a.GetLatestCompletedAccountingRun(ctx)
	if err != nil {
		return nil, err
	}
	return &accountingScope{runID: run.RunID, algorithmVersion: run.AlgorithmVersion}, nil
}

// GetLatestCompletedAccountingRun 获取最新已完成的核算轮次
func (a *AnalyticsService) GetLatestCompletedAccountingRun(ctx context.Context) (*AccountingRunRecord, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT
			run_id, algorithm_version, started_at, completed_at, status,
			source_journal_event_count, source_journal_sequence_max,
			source_boundary_json, failed_reason, notes
		FROM accounting_runs
		WHERE status = 'completed'
		ORDER BY started_at DESC LIMIT 1;
	`)

	var r AccountingRunRecord
	var compStr, failedReason, notes sql.NullString
	var startStr string
	var boundarySeq sql.NullInt64

	err := row.Scan(
		&r.RunID, &r.AlgorithmVersion, &startStr, &compStr, &r.Status,
		&r.SourceJournalEventCount, &boundarySeq,
		&r.SourceBoundaryJSON, &failedReason, &notes,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoCompletedAccountingRun
		}
		return nil, fmt.Errorf("failed to query latest accounting run: %w", err)
	}

	r.StartedAt, _ = time.Parse(time.RFC3339Nano, startStr)
	if compStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, compStr.String)
		r.CompletedAt = &t
	}
	if boundarySeq.Valid {
		r.SourceJournalSequenceMax = &boundarySeq.Int64
	}
	r.FailedReason = failedReason.String
	r.Notes = notes.String

	return &r, nil
}

// GetAccountingFreshness 获取最新核算轮次与当前底层 Journal 事实之间的落后差距 (F3)
// 当存在 active 的 v2 generation 时以 published boundary 为准；否则回退到
// 最新 legacy completed run。
func (a *AnalyticsService) GetAccountingFreshness(ctx context.Context) (*AccountingFreshness, error) {
	var currentMax int64
	if err := a.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(journal_sequence), 0) FROM event_journal;").Scan(&currentMax); err != nil {
		return nil, fmt.Errorf("failed to query current journal max sequence: %w", err)
	}

	gen, err := GetActiveAccountingGeneration(ctx, a.db)
	if err != nil && !errors.Is(err, ErrNoActiveGeneration) && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if gen != nil {
		lag := currentMax - gen.PublishedJournalSequence
		if lag < 0 {
			lag = 0
		}
		return &AccountingFreshness{
			RunID:                     gen.GenerationID,
			SourceJournalSequenceMax:  gen.PublishedJournalSequence,
			CurrentJournalSequenceMax: currentMax,
			LagEvents:                 lag,
			IsFresh:                   lag == 0,
			CompletedAt:               gen.ActivatedAt,
		}, nil
	}

	run, err := a.GetLatestCompletedAccountingRun(ctx)
	if err != nil {
		if errors.Is(err, ErrNoCompletedAccountingRun) {
			return &AccountingFreshness{
				RunID:                     "",
				SourceJournalSequenceMax:  0,
				CurrentJournalSequenceMax: currentMax,
				LagEvents:                 currentMax,
				IsFresh:                   currentMax == 0,
			}, nil
		}
		return nil, err
	}

	sourceMax := int64(0)
	if run.SourceJournalSequenceMax != nil {
		sourceMax = *run.SourceJournalSequenceMax
	}

	lag := currentMax - sourceMax
	if lag < 0 {
		lag = 0
	}

	return &AccountingFreshness{
		RunID:                     run.RunID,
		SourceJournalSequenceMax:  sourceMax,
		CurrentJournalSequenceMax: currentMax,
		LagEvents:                 lag,
		IsFresh:                   lag == 0,
		CompletedAt:               run.CompletedAt,
	}, nil
}

// GetUsageSummary 根据已发布的最新核算数据返回全局用量与质量摘要 (半开区间 [start, end)，错误向上传播)
func (a *AnalyticsService) GetUsageSummary(ctx context.Context, filter AnalyticsFilter) (*UsageSummary, error) {
	scope, err := a.resolveAccountingScope(ctx)
	if err != nil {
		return nil, err
	}
	table, keyCol, keyVal := scope.accountedTable()

	rows, err := a.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			observed_at, interval_start, interval_end, precision, route,
			raw_upload, raw_download, accounted_upload, accounted_download, accounting_class
		FROM %s
		WHERE %s = ?;
	`, table, keyCol), keyVal)
	if err != nil {
		return nil, fmt.Errorf("failed to query accounted traffic: %w", err)
	}
	defer rows.Close()

	var summary UsageSummary
	summary.AccountingVersion = scope.algorithmVersion

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
			return nil, fmt.Errorf("failed to scan accounted traffic row: %w", err)
		}

		if filter.Route != "" && route != string(filter.Route) {
			continue
		}

		obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
		obsTime = obsTime.UTC()

		var effRawUp, effRawDown, effAccUp, effAccDown int64

		if prec == "exact_snapshot" || !intStart.Valid || !intEnd.Valid {
			// 半开区间 [qStart, qEnd): start <= obsTime < end (F0.3)
			inRange := true
			if qStart != nil && obsTime.Before(*qStart) {
				inRange = false
			}
			if qEnd != nil && !obsTime.Before(*qEnd) {
				inRange = false
			}
			if inRange {
				effRawUp = rawUp
				effRawDown = rawDown
				effAccUp = accUp
				effAccDown = accDown
			}
		} else {
			// IntervalAllocator: 半开区间 [wStart, wEnd)
			sTime, _ := time.Parse(time.RFC3339Nano, intStart.String)
			eTime, _ := time.Parse(time.RFC3339Nano, intEnd.String)
			sTime = sTime.UTC()
			eTime = eTime.UTC()

			wStart := sTime
			if qStart != nil && qStart.After(wStart) {
				wStart = *qStart
			}
			wEnd := eTime
			if qEnd != nil && qEnd.Before(wEnd) {
				wEnd = *qEnd
			}

			if wEnd.After(wStart) {
				effRawUp = NewIntervalAllocator(sTime, eTime, rawUp).Allocate(wStart, wEnd)
				effRawDown = NewIntervalAllocator(sTime, eTime, rawDown).Allocate(wStart, wEnd)
				effAccUp = NewIntervalAllocator(sTime, eTime, accUp).Allocate(wStart, wEnd)
				effAccDown = NewIntervalAllocator(sTime, eTime, accDown).Allocate(wStart, wEnd)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading accounted traffic: %w", err)
	}

	// 聚合 Sampling Residuals (半开区间，错误向上传播)
	var resWhere []string
	var resArgs []any
	if filter.StartTime != nil {
		resWhere = append(resWhere, "observed_at >= ?")
		resArgs = append(resArgs, filter.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if filter.EndTime != nil {
		resWhere = append(resWhere, "observed_at < ?")
		resArgs = append(resArgs, filter.EndTime.UTC().Format(time.RFC3339Nano))
	}
	resWhereSQL := ""
	if len(resWhere) > 0 {
		resWhereSQL = "WHERE " + strings.Join(resWhere, " AND ")
	}
	if err := a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(residual_upload), 0), COALESCE(SUM(residual_download), 0)
		FROM residual_intervals %s;
	`, resWhereSQL), resArgs...).Scan(&summary.SamplingResidualUpload, &summary.SamplingResidualDownload); err != nil {
		return nil, fmt.Errorf("failed to query sampling residuals: %w", err)
	}

	// 聚合 Controller Gap 物理流量 (复用 IntervalAllocator，错误向上传播)
	gapRows, err := a.db.QueryContext(ctx, `
		SELECT started_at, ended_at, global_gap_upload_delta, global_gap_download_delta
		FROM monitoring_gaps
		WHERE source = 'controller_stream' AND global_gap_upload_delta IS NOT NULL;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query monitoring gaps for traffic: %w", err)
	}
	defer gapRows.Close()

	for gapRows.Next() {
		var gStartStr, gEndStr sql.NullString
		var gUp, gDown sql.NullInt64
		if err := gapRows.Scan(&gStartStr, &gEndStr, &gUp, &gDown); err != nil {
			return nil, fmt.Errorf("failed to scan gap row: %w", err)
		}
		if !gStartStr.Valid || !gUp.Valid || !gDown.Valid {
			continue
		}

		gStart, _ := time.Parse(time.RFC3339Nano, gStartStr.String)
		gStart = gStart.UTC()
		gEnd := time.Now().UTC()
		if gEndStr.Valid && gEndStr.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, gEndStr.String)
			gEnd = t.UTC()
		}

		wStart := gStart
		if qStart != nil && qStart.After(wStart) {
			wStart = *qStart
		}
		wEnd := gEnd
		if qEnd != nil && qEnd.Before(wEnd) {
			wEnd = *qEnd
		}

		if wEnd.After(wStart) {
			summary.ControllerGapPhysicalUpload += NewIntervalAllocator(gStart, gEnd, gUp.Int64).Allocate(wStart, wEnd)
			summary.ControllerGapPhysicalDownload += NewIntervalAllocator(gStart, gEnd, gDown.Int64).Allocate(wStart, wEnd)
		}
	}
	if err := gapRows.Err(); err != nil {
		return nil, fmt.Errorf("error reading monitoring gaps: %w", err)
	}

	// 计算 Coverage 与 Freshness (错误向上传播)
	cov, err := a.GetCoverage(ctx, filter.StartTime, filter.EndTime)
	if err != nil {
		return nil, fmt.Errorf("failed to compute coverage: %w", err)
	}
	summary.Coverage = cov

	freshness, err := a.GetAccountingFreshness(ctx)
	if err == nil {
		summary.Freshness = freshness
	}

	return &summary, nil
}

// GetTopDimensions 通用多维聚合排行查询 (全窗口 Distinct 连接数与 IntervalAllocator 半开区间精确分摊)
func (a *AnalyticsService) GetTopDimensions(ctx context.Context, dimType string, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	scope, err := a.resolveAccountingScope(ctx)
	if err != nil {
		return nil, err
	}
	table, keyCol, keyVal := scope.accountedTable()

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
		FROM %s
		WHERE %s = ? AND %s IS NOT NULL AND %s != '';
	`, dimColumn, table, keyCol, dimColumn, dimColumn), keyVal)
	if err != nil {
		return nil, fmt.Errorf("failed to query top dimensions: %w", err)
	}
	defer rows.Close()

	type itemKey struct {
		key   string
		route types.RouteType
	}
	type itemAgg struct {
		up, down           int64
		exactUp, exactDown int64
		estUp, estDown     int64
		distinctConns      map[string]bool
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
			return nil, fmt.Errorf("failed to scan top dimension row: %w", err)
		}

		if filter.Route != "" && route != string(filter.Route) {
			continue
		}

		obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
		obsTime = obsTime.UTC()

		var effUp, effDown int64
		var isExact bool

		if prec == "exact_snapshot" || !intStart.Valid || !intEnd.Valid {
			// 半开区间 [qStart, qEnd)
			inRange := true
			if qStart != nil && obsTime.Before(*qStart) {
				inRange = false
			}
			if qEnd != nil && !obsTime.Before(*qEnd) {
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

			wStart := sTime
			if qStart != nil && qStart.After(wStart) {
				wStart = *qStart
			}
			wEnd := eTime
			if qEnd != nil && qEnd.Before(wEnd) {
				wEnd = *qEnd
			}

			if wEnd.After(wStart) {
				effUp = NewIntervalAllocator(sTime, eTime, accUp).Allocate(wStart, wEnd)
				effDown = NewIntervalAllocator(sTime, eTime, accDown).Allocate(wStart, wEnd)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading top dimensions: %w", err)
	}

	var results []TopDimensionItem
	for k, v := range aggMap {
		results = append(results, TopDimensionItem{
			Key:                    k.key,
			Route:                  k.route,
			UploadBytes:            v.up,
			DownloadBytes:          v.down,
			TotalBytes:             v.up + v.down,
			ConnectionCount:        int64(len(v.distinctConns)),
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

func (a *AnalyticsService) GetTopProcesses(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "process", filter)
}

func (a *AnalyticsService) GetTopHosts(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "host", filter)
}

func (a *AnalyticsService) GetTopRules(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "rule", filter)
}

// GetTopRulesDetailed 查询指定窗口内的规则使用排行（包含 (rule, rulePayload, route) + bytes + connectionCount）
func (a *AnalyticsService) GetTopRulesDetailed(ctx context.Context, filter AnalyticsFilter) ([]TopRuleItem, error) {
	scope, err := a.resolveAccountingScope(ctx)
	if err != nil {
		return nil, err
	}
	table, keyCol, keyVal := scope.accountedTable()

	rows, err := a.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			session_id, epoch_id, connection_id, observed_at,
			interval_start, interval_end, precision, route,
			accounted_upload, accounted_download,
			COALESCE(rule, '') AS rule_name,
			COALESCE(rule_payload, '') AS rule_payload_val
		FROM %s
		WHERE %s = ? AND rule IS NOT NULL AND rule != '';
	`, table, keyCol), keyVal)
	if err != nil {
		return nil, fmt.Errorf("failed to query top rules: %w", err)
	}
	defer rows.Close()

	type ruleKey struct {
		rule        string
		rulePayload string
		route       types.RouteType
	}
	type ruleAgg struct {
		up, down           int64
		exactUp, exactDown int64
		estUp, estDown     int64
		distinctConns      map[string]bool
	}

	aggMap := make(map[ruleKey]*ruleAgg)

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
		var sessID, connID, obsAtStr, prec, route, rName, rPayload string
		var epochID int
		var intStart, intEnd sql.NullString
		var accUp, accDown int64

		if err := rows.Scan(
			&sessID, &epochID, &connID, &obsAtStr,
			&intStart, &intEnd, &prec, &route,
			&accUp, &accDown, &rName, &rPayload,
		); err != nil {
			return nil, fmt.Errorf("failed to scan top rules row: %w", err)
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
			if qEnd != nil && !obsTime.Before(*qEnd) {
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

			wStart := sTime
			if qStart != nil && qStart.After(wStart) {
				wStart = *qStart
			}
			wEnd := eTime
			if qEnd != nil && qEnd.Before(wEnd) {
				wEnd = *qEnd
			}

			if wEnd.After(wStart) {
				effUp = NewIntervalAllocator(sTime, eTime, accUp).Allocate(wStart, wEnd)
				effDown = NewIntervalAllocator(sTime, eTime, accDown).Allocate(wStart, wEnd)
				isExact = false
			}
		}

		if effUp == 0 && effDown == 0 {
			continue
		}

		k := ruleKey{rule: rName, rulePayload: rPayload, route: types.RouteType(route)}
		agg, ok := aggMap[k]
		if !ok {
			agg = &ruleAgg{distinctConns: make(map[string]bool)}
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading top rules: %w", err)
	}

	var results []TopRuleItem
	for k, v := range aggMap {
		results = append(results, TopRuleItem{
			Rule:                   k.rule,
			RulePayload:            k.rulePayload,
			Route:                  k.route,
			UploadBytes:            v.up,
			DownloadBytes:          v.down,
			TotalBytes:             v.up + v.down,
			ConnectionCount:        int64(len(v.distinctConns)),
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

func (a *AnalyticsService) GetTopFinalProxies(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "final_proxy", filter)
}

func (a *AnalyticsService) GetProtocolBreakdown(ctx context.Context, filter AnalyticsFilter) ([]TopDimensionItem, error) {
	return a.GetTopDimensions(ctx, "network", filter)
}

// GetCoverage 计算指定时间窗口内的监控覆盖度与缺口并集 (支持心跳超时 liveness 动态判定，F4)
func (a *AnalyticsService) GetCoverage(ctx context.Context, start, end *time.Time) (*CoverageSummary, error) {
	var firstSessStartStr sql.NullString
	err := a.db.QueryRowContext(ctx, "SELECT MIN(started_at) FROM collector_sessions;").Scan(&firstSessStartStr)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("failed to query initial session start: %w", err)
		}
	}
	if !firstSessStartStr.Valid || firstSessStartStr.String == "" {
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

	// 1. Future Query Clip: 如果请求完全在未来
	if reqStart.After(now) {
		summary.FutureDurationMs = reqEnd.Sub(reqStart).Milliseconds()
		summary.CoverageRatio = nil
		return summary, nil
	}

	effEnd := reqEnd
	if effEnd.After(now) {
		summary.FutureDurationMs = effEnd.Sub(now).Milliseconds()
		effEnd = now
	}

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

	// 2. 读取所有与 [effStart, effEnd) 重叠的数据库记录 Gaps
	rows, err := a.db.QueryContext(ctx, `
		SELECT source, started_at, ended_at, reason
		FROM monitoring_gaps
		WHERE (ended_at > ? OR ended_at IS NULL) AND started_at < ?;
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
			return nil, fmt.Errorf("failed to scan gap row: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading monitoring gaps for coverage: %w", err)
	}

	// 3. 最新 Session 状态与心跳 Liveness 动态推导 (F4)
	var latestStatus, latestEndedStr, latestLastEventStr, latestHeartbeatStr sql.NullString
	var heartbeatIntervalMs sql.NullInt64
	err = a.db.QueryRowContext(ctx, `
		SELECT status, ended_at, last_event_at, last_heartbeat_at, heartbeat_interval_ms
		FROM collector_sessions
		ORDER BY started_at DESC LIMIT 1;
	`).Scan(&latestStatus, &latestEndedStr, &latestLastEventStr, &latestHeartbeatStr, &heartbeatIntervalMs)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to query latest collector session: %w", err)
	}

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
		} else if latestStatus.String == string(SessionStatusRunning) {
			// 如果处于 Running 状态，检查心跳是否超时 (F4.4)
			if latestHeartbeatStr.Valid && latestHeartbeatStr.String != "" {
				lastHb, _ := time.Parse(time.RFC3339Nano, latestHeartbeatStr.String)
				lastHb = lastHb.UTC()
				hbIntMs := int64(5000)
				if heartbeatIntervalMs.Valid && heartbeatIntervalMs.Int64 > 0 {
					hbIntMs = heartbeatIntervalMs.Int64
				}
				graceMs := hbIntMs * 3
				if graceMs < 15000 {
					graceMs = 15000
				}
				graceDur := time.Duration(graceMs) * time.Millisecond

				staleThreshold := lastHb.Add(graceDur)
				if now.After(staleThreshold) {
					// 心跳已超时！判定产生 runtime liveness gap
					gapStart := staleThreshold
					if gapStart.Before(effStart) {
						gapStart = effStart
					}
					if effEnd.After(gapStart) {
						dur := effEnd.Sub(gapStart).Milliseconds()
						offGapMs += dur
						rawIntervals = append(rawIntervals, rawInterval{
							start: gapStart, end: effEnd,
							source: "collector_runtime_liveness",
							reason: "collector_heartbeat_stale",
						})
					}
				}
			}
		}
	}

	summary.ControllerGapDurationMs = ctrlGapMs
	summary.CollectorOfflineDurationMs = offGapMs

	// 4. 执行区间求并集 (Interval Union，保留 Provenance)
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
