package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// TemporalFindingKind is a stable, non-localized Phase 4B1 detector name.
type TemporalFindingKind string

const (
	TemporalProcessNewlyObservedOnProxy TemporalFindingKind = "process_newly_observed_on_proxy"
	TemporalProcessProxyGrowth          TemporalFindingKind = "process_proxy_growth"
)

// ComparisonStatus describes whether both effective hourly windows are safe to
// compare. A coverage failure is a valid 200 response state, not a query error.
type ComparisonStatus string

const (
	ComparisonReady                     ComparisonStatus = "ready"
	ComparisonBaselineOutsideKnownScope ComparisonStatus = "baseline_outside_known_scope"
	ComparisonBaselineHasMonitoringGaps ComparisonStatus = "baseline_has_monitoring_gaps"
	ComparisonBaselineFuture            ComparisonStatus = "baseline_future"
	ComparisonRecentOutsideKnownScope   ComparisonStatus = "recent_outside_known_scope"
	ComparisonRecentHasMonitoringGaps   ComparisonStatus = "recent_has_monitoring_gaps"
	ComparisonRecentFuture              ComparisonStatus = "recent_future"
)

const (
	ProcessChangesDefaultLimit = 20
	ProcessChangesMaxLimit     = 50
)

var (
	ErrInvalidProcessChangeRange = errors.New("invalid process change comparison range")
	ErrInvalidProcessChangeLimit = errors.New("invalid process change limit")
)

// ProcessChangeFilter is deliberately explicit: the API caller supplies all
// four effective UTC-hour boundaries and the backend never guesses a baseline.
type ProcessChangeFilter struct {
	BaselineFrom *time.Time
	BaselineTo   *time.Time
	RecentFrom   *time.Time
	RecentTo     *time.Time
	LimitPerKind int
}

type ComparisonWindowEvidence struct {
	From                time.Time `json:"from"`
	To                  time.Time `json:"to"`
	DurationMs          int64     `json:"durationMs"`
	CoveredDurationMs   int64     `json:"coveredDurationMs"`
	UncoveredDurationMs int64     `json:"uncoveredDurationMs"`
	OutsideKnownScopeMs int64     `json:"outsideKnownScopeMs"`
	FutureDurationMs    int64     `json:"futureDurationMs"`
	CoverageRatio       *float64  `json:"coverageRatio,omitempty"`
}

type RouteTrafficEvidence struct {
	UploadBytes            int64 `json:"uploadBytes"`
	DownloadBytes          int64 `json:"downloadBytes"`
	TotalBytes             int64 `json:"totalBytes"`
	ExactUploadBytes       int64 `json:"exactUploadBytes"`
	ExactDownloadBytes     int64 `json:"exactDownloadBytes"`
	EstimatedUploadBytes   int64 `json:"estimatedUploadBytes"`
	EstimatedDownloadBytes int64 `json:"estimatedDownloadBytes"`
}

type ProcessPeriodEvidence struct {
	Proxy  RouteTrafficEvidence `json:"proxy"`
	Direct RouteTrafficEvidence `json:"direct"`
	Reject RouteTrafficEvidence `json:"reject"`
}

type ProcessChangeFinding struct {
	ID                        string                `json:"id"`
	Kind                      TemporalFindingKind   `json:"kind"`
	Process                   string                `json:"process"`
	Baseline                  ProcessPeriodEvidence `json:"baseline"`
	Recent                    ProcessPeriodEvidence `json:"recent"`
	BaselineProxyBytesPerHour float64               `json:"baselineProxyBytesPerHour,omitempty"`
	RecentProxyBytesPerHour   float64               `json:"recentProxyBytesPerHour,omitempty"`
	DeltaBytesPerHour         float64               `json:"deltaBytesPerHour,omitempty"`
	GrowthRatio               *float64              `json:"growthRatio,omitempty"`
}

type ProcessChangeResult struct {
	Status            ComparisonStatus            `json:"status"`
	AccountingVersion string                      `json:"accountingVersion"`
	Baseline          ComparisonWindowEvidence    `json:"baseline"`
	Recent            ComparisonWindowEvidence    `json:"recent"`
	Items             []ProcessChangeFinding      `json:"items"`
	CountsByKind      map[TemporalFindingKind]int `json:"countsByKind"`
	LimitPerKind      int                         `json:"limitPerKind"`
}

type processHourlyTotals struct {
	proxy  RouteTrafficEvidence
	direct RouteTrafficEvidence
	reject RouteTrafficEvidence
}

type processHourlyRow struct {
	process string
	route   types.RouteType
	totals  RouteTrafficEvidence
}

func (s *accountingScope) temporalWindowQuery() (string, string, string) {
	return s.hourlyDimensionsTable()
}

// ListProcessChanges compares process-only hourly dimensions. It intentionally
// never reads accounted traffic rows and never writes storage.
func (s *AuditIntelligenceService) ListProcessChanges(ctx context.Context, filter ProcessChangeFilter) (*ProcessChangeResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("temporal intelligence requires a database")
	}
	if err := validateProcessChangeFilter(filter); err != nil {
		return nil, err
	}
	limit := filter.LimitPerKind
	if limit == 0 {
		limit = ProcessChangesDefaultLimit
	}
	if limit < 1 || limit > ProcessChangesMaxLimit {
		return nil, ErrInvalidProcessChangeLimit
	}

	baselineFrom := filter.BaselineFrom.UTC()
	baselineTo := filter.BaselineTo.UTC()
	recentFrom := filter.RecentFrom.UTC()
	recentTo := filter.RecentTo.UTC()

	analytics := NewAnalyticsService(s.db)
	scope, err := analytics.resolveAccountingScope(ctx)
	if err != nil {
		return nil, err
	}

	baselineCoverage, err := analytics.GetCoverage(ctx, &baselineFrom, &baselineTo)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate baseline comparison coverage: %w", err)
	}
	recentCoverage, err := analytics.GetCoverage(ctx, &recentFrom, &recentTo)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate recent comparison coverage: %w", err)
	}

	result := &ProcessChangeResult{
		Status:            ComparisonReady,
		AccountingVersion: scope.algorithmVersion,
		Baseline:          comparisonWindowEvidence(baselineFrom, baselineTo, baselineCoverage),
		Recent:            comparisonWindowEvidence(recentFrom, recentTo, recentCoverage),
		Items:             make([]ProcessChangeFinding, 0),
		CountsByKind: map[TemporalFindingKind]int{
			TemporalProcessNewlyObservedOnProxy: 0,
			TemporalProcessProxyGrowth:          0,
		},
		LimitPerKind: limit,
	}

	if status := comparisonCoverageStatus("baseline", baselineCoverage); status != ComparisonReady {
		result.Status = status
		return result, nil
	}
	if status := comparisonCoverageStatus("recent", recentCoverage); status != ComparisonReady {
		result.Status = status
		return result, nil
	}

	table, keyColumn, keyValue := scope.temporalWindowQuery()
	baseline, err := s.loadProcessHourlyTotals(ctx, table, keyColumn, keyValue, baselineFrom, baselineTo)
	if err != nil {
		return nil, err
	}
	recent, err := s.loadProcessHourlyTotals(ctx, table, keyColumn, keyValue, recentFrom, recentTo)
	if err != nil {
		return nil, err
	}

	newlyObserved := make([]ProcessChangeFinding, 0)
	growth := make([]ProcessChangeFinding, 0)
	baselineHours := baselineTo.Sub(baselineFrom).Hours()
	recentHours := recentTo.Sub(recentFrom).Hours()

	processes := make(map[string]struct{}, len(baseline)+len(recent))
	for process := range baseline {
		processes[process] = struct{}{}
	}
	for process := range recent {
		processes[process] = struct{}{}
	}
	for process := range processes {
		base := baseline[process]
		current := recent[process]
		if current.proxy.TotalBytes > 0 && base.proxy.TotalBytes == 0 {
			newlyObserved = append(newlyObserved, ProcessChangeFinding{
				ID:       temporalFindingID(TemporalProcessNewlyObservedOnProxy, process),
				Kind:     TemporalProcessNewlyObservedOnProxy,
				Process:  process,
				Baseline: processPeriodEvidence(base),
				Recent:   processPeriodEvidence(current),
			})
		}
		if base.proxy.TotalBytes > 0 && current.proxy.TotalBytes > 0 {
			baseRate := float64(base.proxy.TotalBytes) / baselineHours
			currentRate := float64(current.proxy.TotalBytes) / recentHours
			if currentRate > baseRate {
				delta := currentRate - baseRate
				ratio := delta / baseRate
				growth = append(growth, ProcessChangeFinding{
					ID:                        temporalFindingID(TemporalProcessProxyGrowth, process),
					Kind:                      TemporalProcessProxyGrowth,
					Process:                   process,
					Baseline:                  processPeriodEvidence(base),
					Recent:                    processPeriodEvidence(current),
					BaselineProxyBytesPerHour: baseRate,
					RecentProxyBytesPerHour:   currentRate,
					DeltaBytesPerHour:         delta,
					GrowthRatio:               &ratio,
				})
			}
		}
	}

	sort.Slice(newlyObserved, func(i, j int) bool {
		if newlyObserved[i].Recent.Proxy.TotalBytes != newlyObserved[j].Recent.Proxy.TotalBytes {
			return newlyObserved[i].Recent.Proxy.TotalBytes > newlyObserved[j].Recent.Proxy.TotalBytes
		}
		return newlyObserved[i].Process < newlyObserved[j].Process
	})
	sort.Slice(growth, func(i, j int) bool {
		if growth[i].DeltaBytesPerHour != growth[j].DeltaBytesPerHour {
			return growth[i].DeltaBytesPerHour > growth[j].DeltaBytesPerHour
		}
		if growth[i].RecentProxyBytesPerHour != growth[j].RecentProxyBytesPerHour {
			return growth[i].RecentProxyBytesPerHour > growth[j].RecentProxyBytesPerHour
		}
		return growth[i].Process < growth[j].Process
	})

	result.CountsByKind[TemporalProcessNewlyObservedOnProxy] = len(newlyObserved)
	result.CountsByKind[TemporalProcessProxyGrowth] = len(growth)
	if len(newlyObserved) > limit {
		newlyObserved = newlyObserved[:limit]
	}
	if len(growth) > limit {
		growth = growth[:limit]
	}
	result.Items = append(result.Items, newlyObserved...)
	result.Items = append(result.Items, growth...)
	return result, nil
}

func validateProcessChangeFilter(filter ProcessChangeFilter) error {
	if filter.BaselineFrom == nil || filter.BaselineTo == nil || filter.RecentFrom == nil || filter.RecentTo == nil {
		return ErrInvalidProcessChangeRange
	}
	values := []*time.Time{filter.BaselineFrom, filter.BaselineTo, filter.RecentFrom, filter.RecentTo}
	for _, value := range values {
		utc := value.UTC()
		if utc.Minute() != 0 || utc.Second() != 0 || utc.Nanosecond() != 0 {
			return ErrInvalidProcessChangeRange
		}
	}
	baselineFrom, baselineTo := filter.BaselineFrom.UTC(), filter.BaselineTo.UTC()
	recentFrom, recentTo := filter.RecentFrom.UTC(), filter.RecentTo.UTC()
	if !baselineTo.After(baselineFrom) || !recentTo.After(recentFrom) {
		return ErrInvalidProcessChangeRange
	}
	if baselineTo.Sub(baselineFrom) < time.Hour || recentTo.Sub(recentFrom) < time.Hour {
		return ErrInvalidProcessChangeRange
	}
	if baselineFrom.Before(recentTo) && recentFrom.Before(baselineTo) {
		return ErrInvalidProcessChangeRange
	}
	return nil
}

func comparisonCoverageStatus(prefix string, coverage *CoverageSummary) ComparisonStatus {
	if coverage == nil {
		if prefix == "baseline" {
			return ComparisonBaselineHasMonitoringGaps
		}
		return ComparisonRecentHasMonitoringGaps
	}
	if coverage.FutureDurationMs > 0 {
		if prefix == "baseline" {
			return ComparisonBaselineFuture
		}
		return ComparisonRecentFuture
	}
	if coverage.OutsideKnownScopeMs > 0 {
		if prefix == "baseline" {
			return ComparisonBaselineOutsideKnownScope
		}
		return ComparisonRecentOutsideKnownScope
	}
	if coverage.UncoveredDurationMs > 0 || coverage.CoverageRatio == nil || *coverage.CoverageRatio != 1.0 {
		if prefix == "baseline" {
			return ComparisonBaselineHasMonitoringGaps
		}
		return ComparisonRecentHasMonitoringGaps
	}
	return ComparisonReady
}

func comparisonWindowEvidence(from, to time.Time, coverage *CoverageSummary) ComparisonWindowEvidence {
	evidence := ComparisonWindowEvidence{
		From:       from.UTC(),
		To:         to.UTC(),
		DurationMs: to.Sub(from).Milliseconds(),
	}
	if coverage == nil {
		return evidence
	}
	evidence.CoveredDurationMs = coverage.CoveredDurationMs
	evidence.UncoveredDurationMs = coverage.UncoveredDurationMs
	evidence.OutsideKnownScopeMs = coverage.OutsideKnownScopeMs
	evidence.FutureDurationMs = coverage.FutureDurationMs
	evidence.CoverageRatio = coverage.CoverageRatio
	return evidence
}

func (s *AuditIntelligenceService) loadProcessHourlyTotals(ctx context.Context, table, keyColumn, keyValue string, from, to time.Time) (map[string]processHourlyTotals, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			dimension_key, route,
			COALESCE(SUM(upload_bytes), 0), COALESCE(SUM(download_bytes), 0),
			COALESCE(SUM(exact_upload_bytes), 0), COALESCE(SUM(exact_download_bytes), 0),
			COALESCE(SUM(estimated_upload_bytes), 0), COALESCE(SUM(estimated_download_bytes), 0)
		FROM %s
		WHERE %s = ?
		  AND dimension_type = 'process'
		  AND bucket_start >= ?
		  AND bucket_start < ?
		GROUP BY dimension_key, route;
	`, table, keyColumn), keyValue, from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("failed to query process hourly dimensions: %w", err)
	}
	defer rows.Close()

	result := make(map[string]processHourlyTotals)
	for rows.Next() {
		var row processHourlyRow
		var route string
		if err := rows.Scan(
			&row.process, &route,
			&row.totals.UploadBytes, &row.totals.DownloadBytes,
			&row.totals.ExactUploadBytes, &row.totals.ExactDownloadBytes,
			&row.totals.EstimatedUploadBytes, &row.totals.EstimatedDownloadBytes,
		); err != nil {
			return nil, fmt.Errorf("failed to scan process hourly dimension: %w", err)
		}
		if row.process == "" {
			continue
		}
		row.route = types.RouteType(route)
		row.totals.TotalBytes = row.totals.UploadBytes + row.totals.DownloadBytes
		period := result[row.process]
		switch row.route {
		case types.RouteProxy:
			period.proxy = addRouteTraffic(period.proxy, row.totals)
		case types.RouteDirect:
			period.direct = addRouteTraffic(period.direct, row.totals)
		case types.RouteReject:
			period.reject = addRouteTraffic(period.reject, row.totals)
		default:
			continue
		}
		result[row.process] = period
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading process hourly dimensions: %w", err)
	}
	return result, nil
}

func addRouteTraffic(left, right RouteTrafficEvidence) RouteTrafficEvidence {
	left.UploadBytes += right.UploadBytes
	left.DownloadBytes += right.DownloadBytes
	left.TotalBytes += right.TotalBytes
	left.ExactUploadBytes += right.ExactUploadBytes
	left.ExactDownloadBytes += right.ExactDownloadBytes
	left.EstimatedUploadBytes += right.EstimatedUploadBytes
	left.EstimatedDownloadBytes += right.EstimatedDownloadBytes
	return left
}

func processPeriodEvidence(t processHourlyTotals) ProcessPeriodEvidence {
	return ProcessPeriodEvidence{Proxy: t.proxy, Direct: t.direct, Reject: t.reject}
}

func temporalFindingID(kind TemporalFindingKind, process string) string {
	return string(kind) + ":" + process
}
