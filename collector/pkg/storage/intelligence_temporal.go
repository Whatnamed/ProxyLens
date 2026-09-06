package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// TemporalFindingKind is a stable, non-localized temporal detector name.
type TemporalFindingKind string

const (
	TemporalProcessNewlyObservedOnProxy TemporalFindingKind = "process_newly_observed_on_proxy"
	TemporalProcessProxyGrowth          TemporalFindingKind = "process_proxy_growth"
	TemporalHostGainedProxyAfterDirect  TemporalFindingKind = "host_gained_proxy_after_direct_baseline"
)

// ComparisonStatus describes whether both effective hourly windows are safe to
// compare. A coverage/publication failure is a valid 200 response state, not
// a query error.
type ComparisonStatus string

const (
	ComparisonReady                         ComparisonStatus = "ready"
	ComparisonBaselineOutsideKnownScope     ComparisonStatus = "baseline_outside_known_scope"
	ComparisonBaselineHasMonitoringGaps     ComparisonStatus = "baseline_has_monitoring_gaps"
	ComparisonBaselineFuture                ComparisonStatus = "baseline_future"
	ComparisonBaselineAccountingIncomplete  ComparisonStatus = "baseline_accounting_incomplete"
	ComparisonRecentOutsideKnownScope       ComparisonStatus = "recent_outside_known_scope"
	ComparisonRecentHasMonitoringGaps       ComparisonStatus = "recent_has_monitoring_gaps"
	ComparisonRecentFuture                  ComparisonStatus = "recent_future"
	ComparisonRecentAccountingIncomplete    ComparisonStatus = "recent_accounting_incomplete"
	ComparisonAccountingBoundaryUnavailable ComparisonStatus = "accounting_boundary_unavailable"
)

const (
	ProcessChangesDefaultLimit   = 20
	ProcessChangesMaxLimit       = 50
	TemporalFindingsDefaultLimit = ProcessChangesDefaultLimit
	TemporalFindingsMaxLimit     = ProcessChangesMaxLimit
)

var (
	ErrInvalidTemporalComparisonRange = errors.New("invalid temporal comparison range")
	ErrInvalidTemporalComparisonLimit = errors.New("invalid temporal comparison limit")

	// Keep the Phase 4B1 error names as aliases so existing callers and tests
	// retain their error identity while the shared contract gets a generic name.
	ErrInvalidProcessChangeRange = ErrInvalidTemporalComparisonRange
	ErrInvalidProcessChangeLimit = ErrInvalidTemporalComparisonLimit
)

// TemporalComparisonBounds is the shared explicit four-boundary contract.
// Callers must provide complete UTC-hour windows; the backend never guesses a
// baseline or compares a partial hour.
type TemporalComparisonBounds struct {
	BaselineFrom *time.Time
	BaselineTo   *time.Time
	RecentFrom   *time.Time
	RecentTo     *time.Time
}

// ProcessChangeFilter is the compatibility Phase 4B1 request shape.
type ProcessChangeFilter struct {
	BaselineFrom *time.Time
	BaselineTo   *time.Time
	RecentFrom   *time.Time
	RecentTo     *time.Time
	LimitPerKind int
}

// TemporalFindingsFilter is the generic Phase 4B2A request shape. It is an
// alias deliberately sharing the compatibility fields and validation.
type TemporalFindingsFilter = ProcessChangeFilter

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

// RoutePeriodEvidence is the common route-period evidence shape for process
// and recorded-host temporal findings. The alias preserves the Phase 4B1 JSON
// and Go type name for existing consumers.
type RoutePeriodEvidence struct {
	Proxy  RouteTrafficEvidence `json:"proxy"`
	Direct RouteTrafficEvidence `json:"direct"`
	Reject RouteTrafficEvidence `json:"reject"`
}

type ProcessPeriodEvidence = RoutePeriodEvidence

type ProcessChangeFinding struct {
	ID                        string                `json:"id"`
	Kind                      TemporalFindingKind   `json:"kind"`
	Process                   string                `json:"process"`
	Baseline                  ProcessPeriodEvidence `json:"baseline"`
	Recent                    ProcessPeriodEvidence `json:"recent"`
	BaselineProxyBytesPerHour float64               `json:"baselineProxyBytesPerHour,omitempty"`
	RecentProxyBytesPerHour   float64               `json:"recentProxyBytesPerHour,omitempty"`
	DeltaBytesPerHour         float64               `json:"deltaBytesPerHour,omitempty"`
	// GrowthRatio is the relative increase: (recentRate - baselineRate) / baselineRate.
	// A value of 0.5 means recent bytes/hour is 50% higher than baseline.
	GrowthRatio *float64 `json:"growthRatio,omitempty"`
}

type HostRouteChangeFinding struct {
	ID       string              `json:"id"`
	Kind     TemporalFindingKind `json:"kind"`
	Host     string              `json:"host"`
	Baseline RoutePeriodEvidence `json:"baseline"`
	Recent   RoutePeriodEvidence `json:"recent"`
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

// TemporalFindingsResult is the coherent Review-facing bundle. Both detector
// families are built from one prepared authority/readiness context and the
// response stays empty whenever that shared context is not ready.
type TemporalFindingsResult struct {
	Status            ComparisonStatus            `json:"status"`
	AccountingVersion string                      `json:"accountingVersion"`
	Baseline          ComparisonWindowEvidence    `json:"baseline"`
	Recent            ComparisonWindowEvidence    `json:"recent"`
	ProcessItems      []ProcessChangeFinding      `json:"processItems"`
	HostItems         []HostRouteChangeFinding    `json:"hostItems"`
	CountsByKind      map[TemporalFindingKind]int `json:"countsByKind"`
	LimitPerKind      int                         `json:"limitPerKind"`
}

type routePeriodTotals struct {
	proxy  RouteTrafficEvidence
	direct RouteTrafficEvidence
	reject RouteTrafficEvidence
}

type temporalComparisonContext struct {
	scope        *accountingScope
	baselineFrom time.Time
	baselineTo   time.Time
	recentFrom   time.Time
	recentTo     time.Time
	baseline     ComparisonWindowEvidence
	recent       ComparisonWindowEvidence
	status       ComparisonStatus
	limitPerKind int
}

func (s *accountingScope) temporalWindowQuery() (string, string, string) {
	return s.hourlyDimensionsTable()
}

// prepareTemporalComparison is the one temporal readiness contract used by
// both the compatibility process endpoint and the generic temporal endpoint:
// bounds -> one authority -> monitoring coverage -> window publication.
func (s *AuditIntelligenceService) prepareTemporalComparison(ctx context.Context, filter ProcessChangeFilter) (*temporalComparisonContext, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("temporal intelligence requires a database")
	}
	if err := validateTemporalComparisonBounds(temporalBoundsFromFilter(filter)); err != nil {
		return nil, err
	}
	limit := filter.LimitPerKind
	if limit == 0 {
		limit = TemporalFindingsDefaultLimit
	}
	if limit < 1 || limit > TemporalFindingsMaxLimit {
		return nil, ErrInvalidTemporalComparisonLimit
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

	prepared := &temporalComparisonContext{
		scope:        scope,
		baselineFrom: baselineFrom,
		baselineTo:   baselineTo,
		recentFrom:   recentFrom,
		recentTo:     recentTo,
		baseline:     comparisonWindowEvidence(baselineFrom, baselineTo, baselineCoverage),
		recent:       comparisonWindowEvidence(recentFrom, recentTo, recentCoverage),
		status:       ComparisonReady,
		limitPerKind: limit,
	}

	if status := comparisonCoverageStatus("baseline", baselineCoverage); status != ComparisonReady {
		prepared.status = status
		return prepared, nil
	}
	if status := comparisonCoverageStatus("recent", recentCoverage); status != ComparisonReady {
		prepared.status = status
		return prepared, nil
	}
	if !scope.journalBoundaryAvailable {
		prepared.status = ComparisonAccountingBoundaryUnavailable
		return prepared, nil
	}
	baselineIncomplete, err := s.hasUnpublishedJournalEvidence(ctx, scope.journalBoundary, baselineFrom, baselineTo)
	if err != nil {
		return nil, err
	}
	if baselineIncomplete {
		prepared.status = ComparisonBaselineAccountingIncomplete
		return prepared, nil
	}
	recentIncomplete, err := s.hasUnpublishedJournalEvidence(ctx, scope.journalBoundary, recentFrom, recentTo)
	if err != nil {
		return nil, err
	}
	if recentIncomplete {
		prepared.status = ComparisonRecentAccountingIncomplete
		return prepared, nil
	}
	return prepared, nil
}

// ListProcessChanges keeps the accepted Phase 4B1 process-only endpoint and
// semantics while using the shared temporal preparation and dimension loader.
func (s *AuditIntelligenceService) ListProcessChanges(ctx context.Context, filter ProcessChangeFilter) (*ProcessChangeResult, error) {
	prepared, err := s.prepareTemporalComparison(ctx, filter)
	if err != nil {
		return nil, err
	}
	result := newProcessChangeResult(prepared)
	if prepared.status != ComparisonReady {
		return result, nil
	}

	table, keyColumn, keyValue := prepared.scope.temporalWindowQuery()
	baseline, err := s.loadHourlyDimensionTotals(ctx, table, keyColumn, keyValue, []string{"process"}, prepared.baselineFrom, prepared.baselineTo)
	if err != nil {
		return nil, err
	}
	recent, err := s.loadHourlyDimensionTotals(ctx, table, keyColumn, keyValue, []string{"process"}, prepared.recentFrom, prepared.recentTo)
	if err != nil {
		return nil, err
	}
	newlyObserved, growth := buildProcessFindings(
		baseline["process"], recent["process"],
		prepared.baselineTo.Sub(prepared.baselineFrom).Hours(),
		prepared.recentTo.Sub(prepared.recentFrom).Hours(),
	)
	result.CountsByKind[TemporalProcessNewlyObservedOnProxy] = len(newlyObserved)
	result.CountsByKind[TemporalProcessProxyGrowth] = len(growth)
	result.Items = appendLimitedProcessFindings(result.Items, newlyObserved, prepared.limitPerKind)
	result.Items = appendLimitedProcessFindings(result.Items, growth, prepared.limitPerKind)
	return result, nil
}

// ListTemporalFindings is the coherent Review-facing temporal query. It
// resolves authority/readiness once, reads process and recorded-host hourly
// dimensions in one grouped query per effective window, and returns empty
// findings for every non-ready status.
func (s *AuditIntelligenceService) ListTemporalFindings(ctx context.Context, filter TemporalFindingsFilter) (*TemporalFindingsResult, error) {
	prepared, err := s.prepareTemporalComparison(ctx, filter)
	if err != nil {
		return nil, err
	}
	result := newTemporalFindingsResult(prepared)
	if prepared.status != ComparisonReady {
		return result, nil
	}

	table, keyColumn, keyValue := prepared.scope.temporalWindowQuery()
	baseline, err := s.loadHourlyDimensionTotals(ctx, table, keyColumn, keyValue, []string{"process", "host"}, prepared.baselineFrom, prepared.baselineTo)
	if err != nil {
		return nil, err
	}
	recent, err := s.loadHourlyDimensionTotals(ctx, table, keyColumn, keyValue, []string{"process", "host"}, prepared.recentFrom, prepared.recentTo)
	if err != nil {
		return nil, err
	}

	newlyObserved, growth := buildProcessFindings(
		baseline["process"], recent["process"],
		prepared.baselineTo.Sub(prepared.baselineFrom).Hours(),
		prepared.recentTo.Sub(prepared.recentFrom).Hours(),
	)
	hostFindings := buildHostRouteChangeFindings(baseline["host"], recent["host"])

	result.CountsByKind[TemporalProcessNewlyObservedOnProxy] = len(newlyObserved)
	result.CountsByKind[TemporalProcessProxyGrowth] = len(growth)
	result.CountsByKind[TemporalHostGainedProxyAfterDirect] = len(hostFindings)
	result.ProcessItems = appendLimitedProcessFindings(result.ProcessItems, newlyObserved, prepared.limitPerKind)
	result.ProcessItems = appendLimitedProcessFindings(result.ProcessItems, growth, prepared.limitPerKind)
	if len(hostFindings) > prepared.limitPerKind {
		hostFindings = hostFindings[:prepared.limitPerKind]
	}
	result.HostItems = append(result.HostItems, hostFindings...)
	return result, nil
}

func newProcessChangeResult(prepared *temporalComparisonContext) *ProcessChangeResult {
	return &ProcessChangeResult{
		Status:            prepared.status,
		AccountingVersion: prepared.scope.algorithmVersion,
		Baseline:          prepared.baseline,
		Recent:            prepared.recent,
		Items:             make([]ProcessChangeFinding, 0),
		CountsByKind: map[TemporalFindingKind]int{
			TemporalProcessNewlyObservedOnProxy: 0,
			TemporalProcessProxyGrowth:          0,
		},
		LimitPerKind: prepared.limitPerKind,
	}
}

func newTemporalFindingsResult(prepared *temporalComparisonContext) *TemporalFindingsResult {
	return &TemporalFindingsResult{
		Status:            prepared.status,
		AccountingVersion: prepared.scope.algorithmVersion,
		Baseline:          prepared.baseline,
		Recent:            prepared.recent,
		ProcessItems:      make([]ProcessChangeFinding, 0),
		HostItems:         make([]HostRouteChangeFinding, 0),
		CountsByKind: map[TemporalFindingKind]int{
			TemporalProcessNewlyObservedOnProxy: 0,
			TemporalProcessProxyGrowth:          0,
			TemporalHostGainedProxyAfterDirect:  0,
		},
		LimitPerKind: prepared.limitPerKind,
	}
}

func appendLimitedProcessFindings(dst, findings []ProcessChangeFinding, limit int) []ProcessChangeFinding {
	if len(findings) > limit {
		findings = findings[:limit]
	}
	return append(dst, findings...)
}

func validateProcessChangeFilter(filter ProcessChangeFilter) error {
	return validateTemporalComparisonBounds(temporalBoundsFromFilter(filter))
}

func temporalBoundsFromFilter(filter ProcessChangeFilter) TemporalComparisonBounds {
	return TemporalComparisonBounds{
		BaselineFrom: filter.BaselineFrom,
		BaselineTo:   filter.BaselineTo,
		RecentFrom:   filter.RecentFrom,
		RecentTo:     filter.RecentTo,
	}
}

func validateTemporalComparisonBounds(filter TemporalComparisonBounds) error {
	if filter.BaselineFrom == nil || filter.BaselineTo == nil || filter.RecentFrom == nil || filter.RecentTo == nil {
		return ErrInvalidTemporalComparisonRange
	}
	values := []*time.Time{filter.BaselineFrom, filter.BaselineTo, filter.RecentFrom, filter.RecentTo}
	for _, value := range values {
		utc := value.UTC()
		if utc.Minute() != 0 || utc.Second() != 0 || utc.Nanosecond() != 0 {
			return ErrInvalidTemporalComparisonRange
		}
	}
	baselineFrom, baselineTo := filter.BaselineFrom.UTC(), filter.BaselineTo.UTC()
	recentFrom, recentTo := filter.RecentFrom.UTC(), filter.RecentTo.UTC()
	if !baselineTo.After(baselineFrom) || !recentTo.After(recentFrom) {
		return ErrInvalidTemporalComparisonRange
	}
	if baselineTo.Sub(baselineFrom) < time.Hour || recentTo.Sub(recentFrom) < time.Hour {
		return ErrInvalidTemporalComparisonRange
	}
	if baselineTo.After(recentFrom) {
		return ErrInvalidTemporalComparisonRange
	}
	return nil
}

func (s *AuditIntelligenceService) hasUnpublishedJournalEvidence(ctx context.Context, boundary int64, from, to time.Time) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM event_journal
			WHERE journal_sequence > ?
			  AND observed_at >= ?
			  AND observed_at < ?
		);
	`, boundary, from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check unpublished journal evidence: %w", err)
	}
	return exists != 0, nil
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

// loadHourlyDimensionTotals is a bounded, internal whitelist loader. It uses
// the same hourly authority selected by accountingScope and never accepts a
// user-provided table or dimension SQL fragment.
func (s *AuditIntelligenceService) loadHourlyDimensionTotals(ctx context.Context, table, keyColumn, keyValue string, dimensionTypes []string, from, to time.Time) (map[string]map[string]routePeriodTotals, error) {
	if len(dimensionTypes) == 0 {
		return nil, fmt.Errorf("temporal dimension loader requires at least one dimension type")
	}
	seen := make(map[string]struct{}, len(dimensionTypes))
	placeholders := make([]string, 0, len(dimensionTypes))
	args := make([]any, 0, len(dimensionTypes)+3)
	args = append(args, keyValue)
	for _, dimensionType := range dimensionTypes {
		if dimensionType != "process" && dimensionType != "host" {
			return nil, fmt.Errorf("unsupported temporal dimension type %q", dimensionType)
		}
		if _, ok := seen[dimensionType]; ok {
			continue
		}
		seen[dimensionType] = struct{}{}
		placeholders = append(placeholders, "?")
		args = append(args, dimensionType)
	}
	args = append(args, from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			dimension_type, dimension_key, route,
			COALESCE(SUM(upload_bytes), 0), COALESCE(SUM(download_bytes), 0),
			COALESCE(SUM(exact_upload_bytes), 0), COALESCE(SUM(exact_download_bytes), 0),
			COALESCE(SUM(estimated_upload_bytes), 0), COALESCE(SUM(estimated_download_bytes), 0)
		FROM %s
		WHERE %s = ?
		  AND dimension_type IN (%s)
		  AND bucket_start >= ?
		  AND bucket_start < ?
		GROUP BY dimension_type, dimension_key, route;
	`, table, keyColumn, strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query temporal hourly dimensions: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]routePeriodTotals, len(seen))
	for dimensionType := range seen {
		result[dimensionType] = make(map[string]routePeriodTotals)
	}
	for rows.Next() {
		var dimensionType, dimensionKey, route string
		var totals RouteTrafficEvidence
		if err := rows.Scan(
			&dimensionType, &dimensionKey, &route,
			&totals.UploadBytes, &totals.DownloadBytes,
			&totals.ExactUploadBytes, &totals.ExactDownloadBytes,
			&totals.EstimatedUploadBytes, &totals.EstimatedDownloadBytes,
		); err != nil {
			return nil, fmt.Errorf("failed to scan temporal hourly dimension: %w", err)
		}
		if dimensionKey == "" {
			continue
		}
		totals.TotalBytes = totals.UploadBytes + totals.DownloadBytes
		periods, ok := result[dimensionType]
		if !ok {
			continue
		}
		period := periods[dimensionKey]
		switch types.RouteType(route) {
		case types.RouteProxy:
			period.proxy = addRouteTraffic(period.proxy, totals)
		case types.RouteDirect:
			period.direct = addRouteTraffic(period.direct, totals)
		case types.RouteReject:
			period.reject = addRouteTraffic(period.reject, totals)
		default:
			continue
		}
		periods[dimensionKey] = period
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading temporal hourly dimensions: %w", err)
	}
	return result, nil
}

func buildProcessFindings(baseline, recent map[string]routePeriodTotals, baselineHours, recentHours float64) ([]ProcessChangeFinding, []ProcessChangeFinding) {
	newlyObserved := make([]ProcessChangeFinding, 0)
	growth := make([]ProcessChangeFinding, 0)
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
	return newlyObserved, growth
}

func buildHostRouteChangeFindings(baseline, recent map[string]routePeriodTotals) []HostRouteChangeFinding {
	findings := make([]HostRouteChangeFinding, 0)
	for host, current := range recent {
		if host == "" {
			continue
		}
		base := baseline[host]
		if base.direct.TotalBytes <= 0 || base.proxy.TotalBytes != 0 || current.proxy.TotalBytes <= 0 {
			continue
		}
		findings = append(findings, HostRouteChangeFinding{
			ID:       temporalFindingID(TemporalHostGainedProxyAfterDirect, host),
			Kind:     TemporalHostGainedProxyAfterDirect,
			Host:     host,
			Baseline: routePeriodEvidence(base),
			Recent:   routePeriodEvidence(current),
		})
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Recent.Proxy.TotalBytes != findings[j].Recent.Proxy.TotalBytes {
			return findings[i].Recent.Proxy.TotalBytes > findings[j].Recent.Proxy.TotalBytes
		}
		return findings[i].Host < findings[j].Host
	})
	return findings
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

func routePeriodEvidence(t routePeriodTotals) RoutePeriodEvidence {
	return RoutePeriodEvidence{Proxy: t.proxy, Direct: t.direct, Reject: t.reject}
}

func processPeriodEvidence(t routePeriodTotals) ProcessPeriodEvidence {
	return routePeriodEvidence(t)
}

func temporalFindingID(kind TemporalFindingKind, subject string) string {
	return string(kind) + ":" + subject
}
