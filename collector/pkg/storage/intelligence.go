package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// AuditFindingKind is a stable, non-localized Phase 4A detector identifier.
type AuditFindingKind string

const (
	AuditFindingMatchFallback              AuditFindingKind = "match_fallback_proxy"
	AuditFindingBroadUDP                   AuditFindingKind = "broad_udp_proxy"
	AuditFindingIPOnly                     AuditFindingKind = "ip_only_proxy_target"
	AuditFindingLargeConnection            AuditFindingKind = "large_proxy_connection"
	AuditFindingCatalogedBackgroundProcess AuditFindingKind = "cataloged_background_process_proxy"
)

const (
	AuditFindingsDefaultLimit = 20
	AuditFindingsMaxLimit     = 50

	// LargeProxyConnectionThresholdBytes is deliberately binary and named so
	// the API and UI can explain the boundary without duplicating a magic number.
	LargeProxyConnectionThresholdBytes int64 = 100 * 1024 * 1024
)

var (
	ErrInvalidAuditFindingRange = errors.New("invalid audit finding time range")
	ErrInvalidAuditFindingLimit = errors.New("invalid audit finding limit")
)

// AuditFindingFilter is the fixed PROXY-only Review query scope.
type AuditFindingFilter struct {
	StartTime    *time.Time
	EndTime      *time.Time
	LimitPerKind int
}

type AuditFindingSubject struct {
	Process       string `json:"process,omitempty"`
	ProcessPath   string `json:"processPath,omitempty"`
	Host          string `json:"host,omitempty"`
	SniffHost     string `json:"sniffHost,omitempty"`
	DestinationIP string `json:"destinationIp,omitempty"`
	TargetKind    string `json:"targetKind,omitempty"`
	TargetValue   string `json:"targetValue,omitempty"`

	SessionID    string `json:"sessionId,omitempty"`
	EpochID      int    `json:"epochId,omitempty"`
	ConnectionID string `json:"connectionId,omitempty"`
}

type AuditFindingEvidence struct {
	Route       types.RouteType `json:"route"`
	Rule        string          `json:"rule,omitempty"`
	RulePayload string          `json:"rulePayload,omitempty"`
	Network     string          `json:"network,omitempty"`

	UploadBytes     int64 `json:"uploadBytes"`
	DownloadBytes   int64 `json:"downloadBytes"`
	TotalBytes      int64 `json:"totalBytes"`
	ConnectionCount int64 `json:"connectionCount"`

	ExactUploadBytes       int64 `json:"exactUploadBytes"`
	ExactDownloadBytes     int64 `json:"exactDownloadBytes"`
	EstimatedUploadBytes   int64 `json:"estimatedUploadBytes"`
	EstimatedDownloadBytes int64 `json:"estimatedDownloadBytes"`

	ThresholdBytes *int64 `json:"thresholdBytes,omitempty"`
}

type AuditFindingConnectionKey struct {
	SessionID    string `json:"sessionId"`
	EpochID      int    `json:"epochId"`
	ConnectionID string `json:"connectionId"`
}

type AuditFindingKnowledgeSource struct {
	Publisher string `json:"publisher"`
	Title     string `json:"title"`
	URL       string `json:"url"`
}

type AuditFindingKnowledge struct {
	CatalogVersion string                        `json:"catalogVersion"`
	EntryID        string                        `json:"entryId"`
	Category       string                        `json:"category"`
	Publisher      string                        `json:"publisher"`
	Family         string                        `json:"family"`
	MatchBasis     string                        `json:"matchBasis"`
	Sources        []AuditFindingKnowledgeSource `json:"sources"`
}

type AuditFinding struct {
	ID        string                 `json:"id"`
	Kind      AuditFindingKind       `json:"kind"`
	Subject   AuditFindingSubject    `json:"subject"`
	Evidence  AuditFindingEvidence   `json:"evidence"`
	Knowledge *AuditFindingKnowledge `json:"knowledge,omitempty"`

	SampleConnection *AuditFindingConnectionKey `json:"sampleConnection,omitempty"`
}

type AuditFindingResult struct {
	From                    time.Time                `json:"from"`
	To                      time.Time                `json:"to"`
	Route                   types.RouteType          `json:"route"`
	AccountingVersion       string                   `json:"accountingVersion"`
	KnowledgeCatalogVersion string                   `json:"knowledgeCatalogVersion,omitempty"`
	Items                   []AuditFinding           `json:"items"`
	CountsByKind            map[AuditFindingKind]int `json:"countsByKind"`
	LimitPerKind            int                      `json:"limitPerKind"`
}

// AuditIntelligenceService is a read-only, evidence-native detector service.
// It deliberately has no persistence of findings: every result is derived
// from the current reconciled accounting authority.
type AuditIntelligenceService struct {
	db *sql.DB
}

func NewAuditIntelligenceService(db *sql.DB) *AuditIntelligenceService {
	return &AuditIntelligenceService{db: db}
}

type auditTrafficRow struct {
	sourceEventID string
	sessionID     string
	epochID       int
	connectionID  string
	observedAt    time.Time
	intervalStart *time.Time
	intervalEnd   *time.Time
	precision     string
	route         types.RouteType
	accountedUp   int64
	accountedDown int64
	process       string
	processPath   string
	host          string
	sniffHost     string
	destinationIP string
	network       string
	rule          string
	rulePayload   string
}

type auditConnectionIdentity struct {
	sessionID    string
	epochID      int
	connectionID string
}

type auditAggregateKey struct {
	process     string
	targetKind  string
	targetValue string
	rule        string
}

type backgroundAuditKey struct {
	catalogEntryID string
	process        string
	targetKind     string
	targetValue    string
}

type auditAggregate struct {
	subject        AuditFindingSubject
	evidence       AuditFindingEvidence
	knowledge      *AuditFindingKnowledge
	connections    map[auditConnectionIdentity]struct{}
	sample         *AuditFindingConnectionKey
	latestObserved time.Time
}

// ListFindings scans the selected accounting authority once and aggregates all
// Phase 4A detector families plus the embedded background catalog in memory.
// No finding-specific follow-up
// query is issued.
func (s *AuditIntelligenceService) ListFindings(ctx context.Context, filter AuditFindingFilter) (*AuditFindingResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("audit intelligence requires a database")
	}
	if filter.StartTime == nil || filter.EndTime == nil || !filter.EndTime.After(*filter.StartTime) {
		return nil, ErrInvalidAuditFindingRange
	}
	limit := filter.LimitPerKind
	if limit == 0 {
		limit = AuditFindingsDefaultLimit
	}
	if limit < 1 || limit > AuditFindingsMaxLimit {
		return nil, ErrInvalidAuditFindingLimit
	}
	catalog, err := builtInBackgroundProcessCatalog()
	if err != nil {
		return nil, fmt.Errorf("load background process catalog: %w", err)
	}

	analytics := NewAnalyticsService(s.db)
	scope, err := analytics.resolveAccountingScope(ctx)
	if err != nil {
		return nil, err
	}
	table, keyColumn, keyValue := scope.accountedTable()

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			source_event_id, session_id, epoch_id, connection_id, observed_at,
			interval_start, interval_end, precision, route,
			accounted_upload, accounted_download,
			process, process_path, host, sniff_host, destination_ip, network, rule, rule_payload
		FROM %s
		WHERE %s = ?
		ORDER BY observed_at ASC, source_event_id ASC;
	`, table, keyColumn), keyValue)
	if err != nil {
		return nil, fmt.Errorf("failed to query accounted traffic for findings: %w", err)
	}
	defer rows.Close()

	match := make(map[auditAggregateKey]*auditAggregate)
	udp := make(map[auditAggregateKey]*auditAggregate)
	ipOnly := make(map[auditAggregateKey]*auditAggregate)
	large := make(map[auditConnectionIdentity]*auditAggregate)
	background := make(map[backgroundAuditKey]*auditAggregate)
	from := filter.StartTime.UTC()
	to := filter.EndTime.UTC()

	for rows.Next() {
		row, err := scanAuditTrafficRow(rows)
		if err != nil {
			return nil, err
		}
		up, down, exact, inWindow, err := allocateAuditWindow(row, from, to)
		if err != nil {
			return nil, err
		}
		if !inWindow || (up == 0 && down == 0) {
			continue
		}
		if row.route != types.RouteProxy {
			continue
		}

		row.network = strings.ToLower(strings.TrimSpace(row.network))
		normalizedRule := normalizeAuditRule(row.rule)
		targetKind, targetValue := auditTarget(row)
		identity := auditConnectionIdentity{sessionID: row.sessionID, epochID: row.epochID, connectionID: row.connectionID}

		if normalizedRule == "MATCH" {
			key := auditAggregateKey{process: row.process, targetKind: targetKind, targetValue: targetValue}
			a := ensureAuditAggregate(match, key, row, targetKind, targetValue)
			addAuditEvidence(a, row, up, down, exact)
		}
		if normalizedRule == "NETWORK,udp" && row.network == "udp" {
			key := auditAggregateKey{process: row.process, targetKind: targetKind, targetValue: targetValue, rule: normalizedRule}
			a := ensureAuditAggregate(udp, key, row, targetKind, targetValue)
			addAuditEvidence(a, row, up, down, exact)
		}
		if row.host == "" && row.sniffHost == "" && row.destinationIP != "" {
			key := auditAggregateKey{process: row.process, targetKind: "destination_ip", targetValue: row.destinationIP}
			a := ensureAuditAggregate(ipOnly, key, row, "destination_ip", row.destinationIP)
			addAuditEvidence(a, row, up, down, exact)
		}
		if catalogMatch := catalog.Match(row.process, row.processPath); catalogMatch != nil {
			key := backgroundAuditKey{
				catalogEntryID: catalogMatch.Entry.ID,
				process:        normalizeBackgroundProcessName(row.process),
				targetKind:     targetKind,
				targetValue:    targetValue,
			}
			a := ensureBackgroundAuditAggregate(background, key, row, targetKind, targetValue, catalogMatch, catalog)
			addAuditEvidence(a, row, up, down, exact)
			mergeBackgroundAuditRepresentative(a, row, targetKind, targetValue)
		}

		if a, ok := large[identity]; ok {
			addAuditEvidence(a, row, up, down, exact)
			mergeAuditRepresentative(a, row, targetKind, targetValue)
		} else {
			a := newAuditAggregate(row, targetKind, targetValue)
			addAuditEvidence(a, row, up, down, exact)
			large[identity] = a
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading accounted traffic for findings: %w", err)
	}

	groups := map[AuditFindingKind][]*auditAggregate{
		AuditFindingMatchFallback:              aggregateValues(match),
		AuditFindingBroadUDP:                   aggregateValues(udp),
		AuditFindingIPOnly:                     aggregateValues(ipOnly),
		AuditFindingLargeConnection:            aggregateLargeValues(large),
		AuditFindingCatalogedBackgroundProcess: aggregateValuesByBackgroundKey(background),
	}
	result := &AuditFindingResult{
		From:                    from,
		To:                      to,
		Route:                   types.RouteProxy,
		AccountingVersion:       scope.algorithmVersion,
		KnowledgeCatalogVersion: catalog.Version,
		Items:                   make([]AuditFinding, 0),
		CountsByKind:            make(map[AuditFindingKind]int, len(groups)),
		LimitPerKind:            limit,
	}
	for kind, aggregates := range groups {
		filtered := filterAggregates(kind, aggregates)
		result.CountsByKind[kind] = len(filtered)
		sort.Slice(filtered, func(i, j int) bool {
			left := filtered[i].evidence.TotalBytes
			right := filtered[j].evidence.TotalBytes
			if left != right {
				return left > right
			}
			return auditAggregateSortKey(kind, filtered[i]) < auditAggregateSortKey(kind, filtered[j])
		})
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
		for _, a := range filtered {
			finding := buildAuditFinding(kind, a)
			result.Items = append(result.Items, finding)
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].Evidence.TotalBytes != result.Items[j].Evidence.TotalBytes {
			return result.Items[i].Evidence.TotalBytes > result.Items[j].Evidence.TotalBytes
		}
		return result.Items[i].ID < result.Items[j].ID
	})
	return result, nil
}

type sqlRows interface {
	Scan(dest ...any) error
}

func scanAuditTrafficRow(rows sqlRows) (auditTrafficRow, error) {
	var row auditTrafficRow
	var observed, intervalStart, intervalEnd sql.NullString
	var process, processPath, host, sniffHost, destinationIP, network, rule, rulePayload sql.NullString
	if err := rows.Scan(
		&row.sourceEventID, &row.sessionID, &row.epochID, &row.connectionID, &observed,
		&intervalStart, &intervalEnd, &row.precision, &row.route,
		&row.accountedUp, &row.accountedDown,
		&process, &processPath, &host, &sniffHost, &destinationIP, &network, &rule, &rulePayload,
	); err != nil {
		return auditTrafficRow{}, fmt.Errorf("failed to scan accounted finding row: %w", err)
	}
	var err error
	row.observedAt, err = time.Parse(time.RFC3339Nano, observed.String)
	if err != nil {
		return auditTrafficRow{}, fmt.Errorf("invalid accounted finding observed_at: %w", err)
	}
	row.observedAt = row.observedAt.UTC()
	row.intervalStart, err = parseNullableFindingTime(intervalStart)
	if err != nil {
		return auditTrafficRow{}, err
	}
	row.intervalEnd, err = parseNullableFindingTime(intervalEnd)
	if err != nil {
		return auditTrafficRow{}, err
	}
	row.process = process.String
	row.processPath = processPath.String
	row.host = host.String
	row.sniffHost = sniffHost.String
	row.destinationIP = destinationIP.String
	row.network = network.String
	row.rule = rule.String
	row.rulePayload = rulePayload.String
	return row, nil
}

func parseNullableFindingTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, fmt.Errorf("invalid accounted finding interval: %w", err)
	}
	t = t.UTC()
	return &t, nil
}

func allocateAuditWindow(row auditTrafficRow, from, to time.Time) (up, down int64, exact, inWindow bool, err error) {
	if row.precision == "exact_snapshot" || row.intervalStart == nil || row.intervalEnd == nil {
		if !row.observedAt.Before(from) && row.observedAt.Before(to) {
			return row.accountedUp, row.accountedDown, true, true, nil
		}
		return 0, 0, true, false, nil
	}
	start := *row.intervalStart
	end := *row.intervalEnd
	winStart, winEnd := start, end
	if from.After(winStart) {
		winStart = from
	}
	if to.Before(winEnd) {
		winEnd = to
	}
	if !winEnd.After(winStart) {
		return 0, 0, false, false, nil
	}
	return NewIntervalAllocator(start, end, row.accountedUp).Allocate(winStart, winEnd),
		NewIntervalAllocator(start, end, row.accountedDown).Allocate(winStart, winEnd),
		false, true, nil
}

func normalizeAuditRule(rule string) string {
	normalized := strings.ToUpper(strings.TrimSpace(rule))
	switch normalized {
	case "MATCH":
		return "MATCH"
	case "NETWORK,UDP":
		return "NETWORK,udp"
	default:
		return normalized
	}
}

func auditTarget(row auditTrafficRow) (kind, value string) {
	switch {
	case row.host != "":
		return "host", row.host
	case row.sniffHost != "":
		return "sniff_host", row.sniffHost
	case row.destinationIP != "":
		return "destination_ip", row.destinationIP
	default:
		return "missing", "(missing target evidence)"
	}
}

func newAuditAggregate(row auditTrafficRow, targetKind, targetValue string) *auditAggregate {
	return &auditAggregate{
		subject: AuditFindingSubject{
			Process: row.process, Host: row.host, SniffHost: row.sniffHost,
			DestinationIP: row.destinationIP, TargetKind: targetKind, TargetValue: targetValue,
			SessionID: row.sessionID, EpochID: row.epochID, ConnectionID: row.connectionID,
		},
		evidence:       AuditFindingEvidence{Route: types.RouteProxy, Rule: row.rule, RulePayload: row.rulePayload, Network: row.network},
		connections:    make(map[auditConnectionIdentity]struct{}),
		latestObserved: row.observedAt,
	}
}

func ensureAuditAggregate(groups map[auditAggregateKey]*auditAggregate, key auditAggregateKey, row auditTrafficRow, targetKind, targetValue string) *auditAggregate {
	if a, ok := groups[key]; ok {
		return a
	}
	a := newAuditAggregate(row, targetKind, targetValue)
	groups[key] = a
	return a
}

func ensureBackgroundAuditAggregate(groups map[backgroundAuditKey]*auditAggregate, key backgroundAuditKey, row auditTrafficRow, targetKind, targetValue string, match *BackgroundProcessMatch, catalog *BackgroundProcessCatalog) *auditAggregate {
	if a, ok := groups[key]; ok {
		return a
	}
	a := newBackgroundAuditAggregate(row, targetKind, targetValue, match, catalog)
	groups[key] = a
	return a
}

func addAuditEvidence(a *auditAggregate, row auditTrafficRow, up, down int64, exact bool) {
	a.evidence.UploadBytes += up
	a.evidence.DownloadBytes += down
	a.evidence.TotalBytes += up + down
	if exact {
		a.evidence.ExactUploadBytes += up
		a.evidence.ExactDownloadBytes += down
	} else {
		a.evidence.EstimatedUploadBytes += up
		a.evidence.EstimatedDownloadBytes += down
	}
	identity := auditConnectionIdentity{sessionID: row.sessionID, epochID: row.epochID, connectionID: row.connectionID}
	a.connections[identity] = struct{}{}
	a.evidence.ConnectionCount = int64(len(a.connections))
	if a.sample == nil {
		a.sample = &AuditFindingConnectionKey{SessionID: row.sessionID, EpochID: row.epochID, ConnectionID: row.connectionID}
	}
	if a.evidence.Rule == "" {
		a.evidence.Rule = row.rule
	}
	if a.evidence.RulePayload == "" {
		a.evidence.RulePayload = row.rulePayload
	}
	if a.evidence.Network == "" {
		a.evidence.Network = row.network
	}
}

func mergeAuditRepresentative(a *auditAggregate, row auditTrafficRow, targetKind, targetValue string) {
	if row.observedAt.Before(a.latestObserved) {
		return
	}
	a.latestObserved = row.observedAt
	if row.process != "" {
		a.subject.Process = row.process
	}
	if row.host != "" {
		a.subject.Host = row.host
	}
	if row.sniffHost != "" {
		a.subject.SniffHost = row.sniffHost
	}
	if row.destinationIP != "" {
		a.subject.DestinationIP = row.destinationIP
	}
	if targetKind != "" {
		a.subject.TargetKind = targetKind
	}
	if targetValue != "" {
		a.subject.TargetValue = targetValue
	}
	if row.rule != "" {
		a.evidence.Rule = row.rule
	}
	if row.rulePayload != "" {
		a.evidence.RulePayload = row.rulePayload
	}
	if row.network != "" {
		a.evidence.Network = row.network
	}
}

func newBackgroundAuditAggregate(row auditTrafficRow, targetKind, targetValue string, match *BackgroundProcessMatch, catalog *BackgroundProcessCatalog) *auditAggregate {
	sources := make([]AuditFindingKnowledgeSource, 0, len(match.Entry.SourceIDs))
	for _, sourceID := range match.Entry.SourceIDs {
		source := catalog.Sources[sourceID]
		sources = append(sources, AuditFindingKnowledgeSource{Publisher: source.Publisher, Title: source.Title, URL: source.URL})
	}
	return &auditAggregate{
		subject: AuditFindingSubject{
			Process: row.process, ProcessPath: row.processPath,
			Host: row.host, SniffHost: row.sniffHost, DestinationIP: row.destinationIP,
			TargetKind: targetKind, TargetValue: targetValue,
		},
		evidence: AuditFindingEvidence{Route: types.RouteProxy},
		knowledge: &AuditFindingKnowledge{
			CatalogVersion: match.CatalogVersion,
			EntryID:        match.Entry.ID,
			Category:       match.Entry.Category,
			Publisher:      match.Entry.Publisher,
			Family:         match.Entry.Family,
			MatchBasis:     match.MatchBasis,
			Sources:        sources,
		},
		connections:    make(map[auditConnectionIdentity]struct{}),
		latestObserved: row.observedAt,
	}
}

func mergeBackgroundAuditRepresentative(a *auditAggregate, row auditTrafficRow, targetKind, targetValue string) {
	if row.observedAt.Before(a.latestObserved) {
		return
	}
	mergeAuditRepresentative(a, row, targetKind, targetValue)
	a.subject.ProcessPath = row.processPath
}

func aggregateValues(groups map[auditAggregateKey]*auditAggregate) []*auditAggregate {
	result := make([]*auditAggregate, 0, len(groups))
	for _, value := range groups {
		result = append(result, value)
	}
	return result
}

func aggregateLargeValues(groups map[auditConnectionIdentity]*auditAggregate) []*auditAggregate {
	result := make([]*auditAggregate, 0, len(groups))
	for _, value := range groups {
		result = append(result, value)
	}
	return result
}

func aggregateValuesByBackgroundKey(groups map[backgroundAuditKey]*auditAggregate) []*auditAggregate {
	result := make([]*auditAggregate, 0, len(groups))
	for _, value := range groups {
		result = append(result, value)
	}
	return result
}

func filterAggregates(kind AuditFindingKind, groups []*auditAggregate) []*auditAggregate {
	result := make([]*auditAggregate, 0, len(groups))
	for _, a := range groups {
		if a.evidence.TotalBytes <= 0 {
			continue
		}
		if kind == AuditFindingLargeConnection && a.evidence.TotalBytes <= LargeProxyConnectionThresholdBytes {
			continue
		}
		result = append(result, a)
	}
	return result
}

func buildAuditFinding(kind AuditFindingKind, a *auditAggregate) AuditFinding {
	evidence := a.evidence
	var sample *AuditFindingConnectionKey
	if kind == AuditFindingLargeConnection {
		sample = a.sample
		threshold := LargeProxyConnectionThresholdBytes
		evidence.ThresholdBytes = &threshold
	}
	subject := a.subject
	if kind != AuditFindingLargeConnection {
		subject.SessionID = ""
		subject.EpochID = 0
		subject.ConnectionID = ""
	}
	return AuditFinding{
		ID: findingID(kind, subject, a.knowledge), Kind: kind, Subject: subject, Evidence: evidence,
		Knowledge:        a.knowledge,
		SampleConnection: sample,
	}
}

func auditSubjectKey(kind AuditFindingKind, subject AuditFindingSubject) string {
	b, _ := json.Marshal(struct {
		Kind AuditFindingKind    `json:"kind"`
		S    AuditFindingSubject `json:"subject"`
	}{kind, subject})
	return string(b)
}

func auditAggregateSortKey(kind AuditFindingKind, aggregate *auditAggregate) string {
	if kind == AuditFindingCatalogedBackgroundProcess && aggregate.knowledge != nil {
		return aggregate.knowledge.Family + "\x00" + aggregate.subject.Process + "\x00" + aggregate.subject.TargetKind + "\x00" + aggregate.subject.TargetValue
	}
	return auditSubjectKey(kind, aggregate.subject)
}

func findingID(kind AuditFindingKind, subject AuditFindingSubject, knowledge *AuditFindingKnowledge) string {
	identity := struct {
		Kind           AuditFindingKind `json:"kind"`
		CatalogEntryID string           `json:"catalogEntryId,omitempty"`
		Process        string           `json:"process,omitempty"`
		TargetKind     string           `json:"targetKind,omitempty"`
		TargetValue    string           `json:"targetValue,omitempty"`
		Rule           string           `json:"rule,omitempty"`
		DestinationIP  string           `json:"destinationIp,omitempty"`
		SessionID      string           `json:"sessionId,omitempty"`
		EpochID        int              `json:"epochId,omitempty"`
		ConnectionID   string           `json:"connectionId,omitempty"`
	}{Kind: kind}

	switch kind {
	case AuditFindingMatchFallback:
		identity.Process = subject.Process
		identity.TargetKind = subject.TargetKind
		identity.TargetValue = subject.TargetValue
		identity.Rule = "MATCH"
	case AuditFindingBroadUDP:
		identity.Process = subject.Process
		identity.TargetKind = subject.TargetKind
		identity.TargetValue = subject.TargetValue
		identity.Rule = "NETWORK,udp"
	case AuditFindingIPOnly:
		identity.Process = subject.Process
		identity.DestinationIP = subject.DestinationIP
	case AuditFindingLargeConnection:
		identity.SessionID = subject.SessionID
		identity.EpochID = subject.EpochID
		identity.ConnectionID = subject.ConnectionID
	case AuditFindingCatalogedBackgroundProcess:
		if knowledge != nil {
			identity.CatalogEntryID = knowledge.EntryID
		}
		identity.Process = normalizeBackgroundProcessName(subject.Process)
		identity.TargetKind = subject.TargetKind
		identity.TargetValue = subject.TargetValue
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return "finding_" + hex.EncodeToString(digest[:])
}
