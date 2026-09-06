package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

var intelligenceTestAnchor = time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

type intelligenceFixtureEvent struct {
	id, connectionID, process, host, sniffHost, destinationIP, network, rule, payload string
	route                                                                             types.RouteType
	up, down                                                                          int64
	observedAt                                                                        time.Time
	precision                                                                         string
	intervalStart, intervalEnd                                                        *time.Time
}

func TestAuditIntelligenceDetectorSemanticsAndStableIDs(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "match-a-1", connectionID: "match-a", process: "alpha.exe", host: "fallback.example", destinationIP: "203.0.113.10", network: "tcp", rule: "MATCH", payload: "MATCH", route: types.RouteProxy, up: 100, down: 900, observedAt: intelligenceTestAnchor.Add(10 * time.Minute)},
		{id: "match-a-2", connectionID: "match-a", process: "alpha.exe", host: "fallback.example", destinationIP: "203.0.113.10", network: "tcp", rule: "MATCH", payload: "MATCH", route: types.RouteProxy, up: 100, down: 900, observedAt: intelligenceTestAnchor.Add(11 * time.Minute)},
		{id: "match-tie", connectionID: "match-tie", process: "beta.exe", host: "tie.example", destinationIP: "203.0.113.11", network: "tcp", rule: "match", payload: "MATCH", route: types.RouteProxy, up: 500, down: 500, observedAt: intelligenceTestAnchor.Add(12 * time.Minute)},
		{id: "match-direct", connectionID: "match-direct", process: "direct.exe", host: "direct.example", destinationIP: "203.0.113.12", network: "tcp", rule: "MATCH", payload: "MATCH", route: types.RouteDirect, up: 100000, down: 100000, observedAt: intelligenceTestAnchor.Add(13 * time.Minute)},
		{id: "match-specific", connectionID: "match-specific", process: "specific.exe", host: "specific.example", destinationIP: "203.0.113.13", network: "tcp", rule: "DomainSuffix", payload: "example", route: types.RouteProxy, up: 100000, down: 100000, observedAt: intelligenceTestAnchor.Add(14 * time.Minute)},
		{id: "udp-broad", connectionID: "udp-broad", process: "ntp.exe", host: "pool.example", destinationIP: "198.51.100.20", network: "udp", rule: "NETWORK,udp", payload: "udp", route: types.RouteProxy, up: 20, down: 2000, observedAt: intelligenceTestAnchor.Add(15 * time.Minute)},
		{id: "udp-direct", connectionID: "udp-direct", process: "direct-udp.exe", host: "direct-udp.example", destinationIP: "198.51.100.21", network: "udp", rule: "NETWORK,udp", payload: "udp", route: types.RouteDirect, up: 100000, down: 100000, observedAt: intelligenceTestAnchor.Add(16 * time.Minute)},
		{id: "udp-tcp", connectionID: "udp-tcp", process: "tcp.exe", host: "tcp.example", destinationIP: "198.51.100.22", network: "tcp", rule: "NETWORK,udp", payload: "udp", route: types.RouteProxy, up: 100000, down: 100000, observedAt: intelligenceTestAnchor.Add(17 * time.Minute)},
		{id: "udp-specific", connectionID: "udp-specific", process: "specific-udp.exe", host: "specific-udp.example", destinationIP: "198.51.100.23", network: "udp", rule: "DomainSuffix", payload: "example", route: types.RouteProxy, up: 100000, down: 100000, observedAt: intelligenceTestAnchor.Add(18 * time.Minute)},
		{id: "ip-only", connectionID: "ip-only", process: "ip-client.exe", destinationIP: "192.0.2.30", network: "tcp", rule: "MATCH", payload: "MATCH", route: types.RouteProxy, up: 300, down: 700, observedAt: intelligenceTestAnchor.Add(19 * time.Minute)},
		{id: "same-ip-host", connectionID: "same-ip-host", process: "ip-client.exe", host: "known.example", destinationIP: "192.0.2.30", network: "tcp", rule: "MATCH", payload: "MATCH", route: types.RouteProxy, up: 900, down: 900, observedAt: intelligenceTestAnchor.Add(20 * time.Minute)},
		{id: "large-exact", connectionID: "large-exact", process: "boundary.exe", host: "boundary.example", destinationIP: "192.0.2.40", network: "tcp", rule: "DomainSuffix", payload: "example", route: types.RouteProxy, up: 100 * 1024 * 1024, observedAt: intelligenceTestAnchor.Add(21 * time.Minute)},
		{id: "large-over-1", connectionID: "large-over", process: "downloader.exe", host: "large.example", destinationIP: "192.0.2.41", network: "tcp", rule: "DomainSuffix", payload: "example", route: types.RouteProxy, up: 60 * 1024 * 1024, observedAt: intelligenceTestAnchor.Add(22 * time.Minute)},
		{id: "large-over-2", connectionID: "large-over", process: "downloader.exe", host: "large.example", destinationIP: "192.0.2.41", network: "tcp", rule: "DomainSuffix", payload: "example", route: types.RouteProxy, down: 40*1024*1024 + 1, observedAt: intelligenceTestAnchor.Add(23 * time.Minute)},
	}
	db, cleanup := buildIntelligenceFixture(t, events, false)
	defer cleanup()

	service := NewAuditIntelligenceService(db)
	from := intelligenceTestAnchor
	to := intelligenceTestAnchor.Add(time.Hour)
	result, err := service.ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
	if err != nil {
		t.Fatalf("ListFindings failed: %v", err)
	}
	if result.Route != types.RouteProxy || result.LimitPerKind != AuditFindingsDefaultLimit {
		t.Fatalf("unexpected result scope: %+v", result)
	}
	if result.CountsByKind[AuditFindingMatchFallback] != 4 {
		t.Fatalf("MATCH count mismatch: %+v", result.CountsByKind)
	}
	if result.CountsByKind[AuditFindingBroadUDP] != 1 {
		t.Fatalf("UDP count mismatch: %+v", result.CountsByKind)
	}
	if result.CountsByKind[AuditFindingIPOnly] != 1 {
		t.Fatalf("IP-only count mismatch: %+v", result.CountsByKind)
	}
	if result.CountsByKind[AuditFindingLargeConnection] != 1 {
		t.Fatalf("large connection count mismatch: %+v", result.CountsByKind)
	}

	matchItems := findingsOfKind(result.Items, AuditFindingMatchFallback)
	if len(matchItems) != 4 || matchItems[0].Subject.Process != "alpha.exe" {
		t.Fatalf("MATCH ordering/subject mismatch: %+v", matchItems)
	}
	udpItems := findingsOfKind(result.Items, AuditFindingBroadUDP)
	if len(udpItems) != 1 || udpItems[0].Evidence.Rule != "NETWORK,udp" || udpItems[0].Evidence.Network != "udp" {
		t.Fatalf("UDP evidence mismatch: %+v", udpItems)
	}
	if matchItems[0].Evidence.ConnectionCount != 1 {
		t.Fatalf("MATCH connection count mismatch: %+v", matchItems[0].Evidence)
	}
	largeItems := findingsOfKind(result.Items, AuditFindingLargeConnection)
	if len(largeItems) != 1 || largeItems[0].Evidence.TotalBytes != LargeProxyConnectionThresholdBytes+1 {
		t.Fatalf("large threshold mismatch: %+v", largeItems)
	}
	if largeItems[0].Evidence.ThresholdBytes == nil || *largeItems[0].Evidence.ThresholdBytes != LargeProxyConnectionThresholdBytes {
		t.Fatalf("large threshold evidence missing: %+v", largeItems[0].Evidence)
	}

	idsBefore := findingIDsBySubject(result.Items)
	toLater := to.Add(30 * time.Minute)
	later, err := service.ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &toLater})
	if err != nil {
		t.Fatalf("later ListFindings failed: %v", err)
	}
	if !reflect.DeepEqual(idsBefore, findingIDsBySubject(later.Items)) {
		t.Fatalf("finding IDs changed when only live 'to' advanced: before=%v after=%v", idsBefore, findingIDsBySubject(later.Items))
	}
}

func TestAuditIntelligenceUsesIntervalAllocationAndIgnoresZeroAccountedRows(t *testing.T) {
	intervalStart := intelligenceTestAnchor.Add(30 * time.Minute)
	intervalEnd := intelligenceTestAnchor.Add(90 * time.Minute)
	events := []intelligenceFixtureEvent{
		{id: "interval-match", connectionID: "interval-match", process: "interval.exe", host: "interval.example", destinationIP: "198.51.100.50", network: "tcp", rule: "MATCH", payload: "MATCH", route: types.RouteProxy, up: 1000, down: 2000, observedAt: intervalStart, precision: "interval_derived", intervalStart: &intervalStart, intervalEnd: &intervalEnd},
	}
	db, cleanup := buildIntelligenceFixture(t, events, false)
	defer cleanup()
	service := NewAuditIntelligenceService(db)
	from := intelligenceTestAnchor.Add(time.Hour)
	to := intelligenceTestAnchor.Add(2 * time.Hour)
	result, err := service.ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
	if err != nil {
		t.Fatalf("ListFindings failed: %v", err)
	}
	items := findingsOfKind(result.Items, AuditFindingMatchFallback)
	if len(items) != 1 {
		t.Fatalf("expected one interval finding, got %+v", result.CountsByKind)
	}
	if items[0].Evidence.ExactUploadBytes != 0 || items[0].Evidence.EstimatedUploadBytes != 500 {
		t.Fatalf("interval exact/estimated split mismatch: %+v", items[0].Evidence)
	}
	if items[0].Evidence.EstimatedDownloadBytes != 1000 {
		t.Fatalf("interval download allocation mismatch: %+v", items[0].Evidence)
	}
}

func TestAuditIntelligenceLegacyV2Equivalence(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "eq-match", connectionID: "eq-match", process: "eq.exe", host: "eq.example", destinationIP: "203.0.113.90", network: "tcp", rule: "MATCH", payload: "MATCH", route: types.RouteProxy, up: 100, down: 900, observedAt: intelligenceTestAnchor.Add(5 * time.Minute)},
		{id: "eq-udp", connectionID: "eq-udp", process: "eq-udp.exe", destinationIP: "203.0.113.91", network: "udp", rule: "NETWORK,udp", payload: "udp", route: types.RouteProxy, up: 10, down: 200, observedAt: intelligenceTestAnchor.Add(6 * time.Minute)},
		{id: "eq-large", connectionID: "eq-large", process: "eq-large.exe", host: "eq-large.example", destinationIP: "203.0.113.92", network: "tcp", rule: "DomainSuffix", payload: "example", route: types.RouteProxy, up: LargeProxyConnectionThresholdBytes + 1, observedAt: intelligenceTestAnchor.Add(7 * time.Minute)},
	}
	legacyDB, legacyCleanup := buildIntelligenceFixture(t, events, false)
	defer legacyCleanup()
	v2DB, v2Cleanup := buildIntelligenceFixture(t, events, true)
	defer v2Cleanup()

	from := intelligenceTestAnchor
	to := intelligenceTestAnchor.Add(time.Hour)
	legacy, err := NewAuditIntelligenceService(legacyDB).ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
	if err != nil {
		t.Fatalf("legacy findings failed: %v", err)
	}
	v2, err := NewAuditIntelligenceService(v2DB).ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
	if err != nil {
		t.Fatalf("v2 findings failed: %v", err)
	}
	if !reflect.DeepEqual(legacy.Items, v2.Items) || !reflect.DeepEqual(legacy.CountsByKind, v2.CountsByKind) {
		t.Fatalf("legacy/v2 findings differ:\nlegacy=%+v\nv2=%+v", legacy, v2)
	}
}

func TestAuditIntelligenceScaledTimings(t *testing.T) {
	for _, scale := range []int{10_000, 100_000} {
		t.Run(fmt.Sprintf("events_%d", scale), func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "scaled.db")
			db, err := OpenDB(context.Background(), dbPath)
			if err != nil {
				t.Fatalf("OpenDB failed: %v", err)
			}
			defer db.Close()

			insertStarted := time.Now()
			if err := insertScaledAuditRows(db, scale); err != nil {
				t.Fatalf("insertScaledAuditRows failed: %v", err)
			}
			insertionDuration := time.Since(insertStarted)

			from := intelligenceTestAnchor.Add(-time.Minute)
			to := intelligenceTestAnchor.Add(48 * time.Hour)
			queryStarted := time.Now()
			result, err := NewAuditIntelligenceService(db).ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
			if err != nil {
				t.Fatalf("scaled ListFindings failed: %v", err)
			}
			queryDuration := time.Since(queryStarted)
			if len(result.Items) == 0 || result.CountsByKind[AuditFindingMatchFallback] == 0 {
				t.Fatalf("scaled fixture did not produce findings: %+v", result.CountsByKind)
			}
			t.Logf("audit-scale events=%d insertion=%s seed=precomputed final_incremental_service=%s total=%s result_items=%d", scale, insertionDuration, queryDuration, insertionDuration+queryDuration, len(result.Items))
		})
	}
}

func insertScaledAuditRows(db *sql.DB, scale int) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	anchor := intelligenceTestAnchor
	startedAt := anchor.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO accounting_runs (
			run_id, algorithm_version, started_at, completed_at, status,
			source_journal_event_count, source_boundary_json, notes
		) VALUES (?, ?, ?, ?, 'completed', ?, '{}', ?);
	`, "run-scaled", "legacy-v1", startedAt, startedAt, scale, fmt.Sprintf("scaled timing fixture %d", scale)); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO accounted_traffic (
			run_id, source_event_id, session_id, epoch_id, connection_id, observed_at,
			interval_start, interval_end, precision, route, raw_upload, raw_download,
			accounted_upload, accounted_download, accounting_class, process, process_path,
			host, sniff_host, destination_ip, network, rule, rule_payload, final_proxy,
			top_policy_group, dimension_derivation_version
		) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, 'exact_snapshot', 'PROXY', ?, ?, ?, ?, 'unique', ?, ?, ?, NULL, ?, 'tcp', 'MATCH', 'MATCH', ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := 0; i < scale; i++ {
		observedAt := anchor.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		bytes := int64(1024 + (i % 4096))
		if _, err := stmt.ExecContext(ctx,
			"run-scaled", fmt.Sprintf("scale-event-%06d", i), "sess-scaled", 1,
			fmt.Sprintf("scale-conn-%06d", i), observedAt,
			bytes, bytes*2, bytes, bytes*2,
			"scale.exe", "C:\\Synthetic\\scale.exe", "scale.example", "198.51.100.70",
			"Node-Scale-01", "ScaleGroup", "v1",
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func buildIntelligenceFixture(t *testing.T, events []intelligenceFixtureEvent, v2 bool) (*sql.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "intelligence.db")
	sink, err := OpenSQLiteSink(context.Background(), dbPath, "sess-intelligence", "v1.0.0-test")
	if err != nil {
		t.Fatalf("OpenSQLiteSink failed: %v", err)
	}
	for index, fixture := range events {
		event := &types.CollectorEvent{
			EventID: fixture.id, SessionID: "sess-intelligence", EpochID: 1,
			FrameSequence: int64(index + 1), EventSequence: 1, Timestamp: fixture.observedAt,
			Type: types.EventConnectionNew, ConnectionID: fixture.connectionID,
			Route: fixture.route, AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: fixture.process, Host: fixture.host, SniffHost: fixture.sniffHost, DestinationIP: fixture.destinationIP, Network: fixture.network, DestinationPort: "443"},
			Rule:     fixture.rule, RulePayload: fixture.payload, Chains: []string{"Node-Test", "ProxyGroup"},
			DeltaUpload: fixture.up, DeltaDownload: fixture.down,
			ObservedUploadCounter: fixture.up, ObservedDownloadCounter: fixture.down,
			Precision: fixture.precision,
		}
		if fixture.intervalStart != nil && fixture.intervalEnd != nil {
			event.AttributionInterval = []string{fixture.intervalStart.Format(time.RFC3339Nano), fixture.intervalEnd.Format(time.RFC3339Nano)}
		}
		if err := sink.Emit(event); err != nil {
			t.Fatalf("Emit %s failed: %v", fixture.id, err)
		}
	}
	if err := sink.EndSession(context.Background(), "sess-intelligence", SessionStatusClosedClean); err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close sink failed: %v", err)
	}
	db, err := OpenDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	if v2 {
		if _, err := AdvanceAccountingV2(context.Background(), db, "intelligence v2 fixture", 0); err != nil {
			t.Fatalf("AdvanceAccountingV2 failed: %v", err)
		}
	} else if _, err := RebuildAccounting(context.Background(), db, "intelligence legacy fixture"); err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}
	return db, func() { _ = db.Close() }
}

func findingsOfKind(items []AuditFinding, kind AuditFindingKind) []AuditFinding {
	result := make([]AuditFinding, 0)
	for _, item := range items {
		if item.Kind == kind {
			result = append(result, item)
		}
	}
	return result
}

func findingIDsBySubject(items []AuditFinding) map[string]string {
	result := make(map[string]string)
	for _, item := range items {
		result[string(item.Kind)+":"+item.Subject.TargetKind+":"+item.Subject.TargetValue+":"+item.Subject.Process+":"+item.Subject.ConnectionID] = item.ID
	}
	return result
}
