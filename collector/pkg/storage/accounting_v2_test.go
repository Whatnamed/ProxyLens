package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// -----------------------------------------------------------------------------
// Deterministic closed fixture for legacy-vs-v2 equivalence.
//
// Frame script (single session/epoch):
//   f1      bootstrap proxy-logical + direct-logical (baselines, not accounted)
//   f2      ConnectionNew cand-relay 900/1800 (relay candidate); Delta proxy-logical +400/+800
//   f3-f12  idle zero-byte deltas on both live connections (compacted in v2)
//   --- prefix boundary (legacy would classify cand as unpaired/missing here) ---
//   f13     Delta cand-relay +500/+1000 -> totals match proxy-logical -> confirmed
//   f14     ConnectionNew missing-conn 300/400 (no process/rule/chains -> missing)
//   f15     interval_only Delta proxy-logical +1000/+2000 (reconnect catch-up)
//   f16     MetadataUpdated direct-logical
//   f17     ConnectionDisappeared missing-conn
//   f18     zero-byte Delta proxy-logical
//   f19     ConnectionNew idle-zeros 0/0 (zero-byte-only contributor: the one
//           documented v2 compaction divergence in hourly connection_count)
// -----------------------------------------------------------------------------

const equivalenceFixtureSession = "sess-eq"

func fixtureBase() time.Time {
	return time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
}

type hourlyKey struct{ bucket, dimType, dimKey, route string }

func emitEquivalenceFrame(t *testing.T, s *SQLiteEventSink, frame int64, ts time.Time, emit func(frameSeq int64, evSeq int64) []*types.CollectorEvent) {
	t.Helper()
	events := emit(frame, 0)
	for i, e := range events {
		e.SessionID = equivalenceFixtureSession
		e.EpochID = 1
		e.FrameSequence = frame
		e.EventSequence = int64(i + 1)
		if e.Timestamp.IsZero() {
			e.Timestamp = ts
		}
		e.GenerateDeterministicEventID()
		if err := s.Emit(e); err != nil {
			t.Fatalf("emit frame %d event %d failed: %v", frame, i+1, err)
		}
	}
}

func evNew(connID string, up, down, obsUp, obsDown int64, route types.RouteType, class types.AttributionClass, meta types.RawMetadata, rule string, chains []string) *types.CollectorEvent {
	return &types.CollectorEvent{
		Type: types.EventConnectionNew, ConnectionID: connID,
		DeltaUpload: up, DeltaDownload: down,
		ObservedUploadCounter: obsUp, ObservedDownloadCounter: obsDown,
		MonitoredCumulativeUpload: obsUp, MonitoredCumulativeDownload: obsDown,
		Route: route, AttributionClass: class, Metadata: meta, Rule: rule, Chains: chains,
	}
}

func evDelta(connID string, up, down, obsUp, obsDown int64, route types.RouteType, class types.AttributionClass, meta types.RawMetadata, rule string, chains []string) *types.CollectorEvent {
	return &types.CollectorEvent{
		Type: types.EventConnectionDelta, ConnectionID: connID,
		DeltaUpload: up, DeltaDownload: down,
		ObservedUploadCounter: obsUp, ObservedDownloadCounter: obsDown,
		MonitoredCumulativeUpload: obsUp, MonitoredCumulativeDownload: obsDown,
		Route: route, AttributionClass: class, Metadata: meta, Rule: rule, Chains: chains,
	}
}

var (
	fixtureProxyMeta   = types.RawMetadata{Process: "app.exe", Host: "logical.example", Network: "tcp"}
	fixtureCandMeta    = types.RawMetadata{Host: "relay.example", Network: "tcp"}
	fixtureDirectMeta  = types.RawMetadata{Process: "ntp.exe", Host: "ntp.example", Network: "udp"}
	fixtureMissingMeta = types.RawMetadata{Host: "mystery.example", Network: "tcp"}
	fixtureZeroMeta    = types.RawMetadata{Process: "zero.exe", Host: "zero.example", Network: "tcp"}
)

func emitEquivalenceFrameScript(t *testing.T, s *SQLiteEventSink, frame int64, ts time.Time) {
	t.Helper()
	proxyChains := []string{"NodeA", "GroupX"}
	switch frame {
	case 1:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				{Type: types.EventConnectionBootstrap, ConnectionID: "proxy-logical",
					ObservedUploadCounter: 1000, ObservedDownloadCounter: 2000,
					Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
					Metadata: fixtureProxyMeta, Rule: "MATCH", Chains: proxyChains},
				{Type: types.EventConnectionBootstrap, ConnectionID: "direct-logical",
					ObservedUploadCounter: 10, ObservedDownloadCounter: 20,
					Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
					Metadata: fixtureDirectMeta, Rule: "NETWORK,udp", Chains: []string{"DIRECT"}},
			}
		})
	case 2:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				evNew("cand-relay", 900, 1800, 900, 1800, types.RouteProxy, types.ClassRelayCandidate, fixtureCandMeta, "", proxyChains),
				evDelta("proxy-logical", 400, 800, 1400, 2800, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
			}
		})
	case 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 18:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				evDelta("cand-relay", 0, 0, 900, 1800, types.RouteProxy, types.ClassRelayCandidate, fixtureCandMeta, "", proxyChains),
				evDelta("proxy-logical", 0, 0, 1400, 2800, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
			}
		})
	case 13:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				evDelta("cand-relay", 500, 1000, 1400, 2800, types.RouteProxy, types.ClassRelayCandidate, fixtureCandMeta, "", proxyChains),
			}
		})
	case 14:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				evNew("missing-conn", 300, 400, 300, 400, types.RouteUnknown, types.ClassUnpairedMissingAttr, fixtureMissingMeta, "", nil),
			}
		})
	case 15:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			evs := []*types.CollectorEvent{
				evDelta("proxy-logical", 1000, 2000, 2400, 4800, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
			}
			ivl := fixtureBase().Add(13 * time.Second)
			ivl2 := fixtureBase().Add(14 * time.Second)
			evs[0].AttributionInterval = []string{ivl.Format(time.RFC3339Nano), ivl2.Format(time.RFC3339Nano)}
			evs[0].Precision = "interval_only"
			return evs
		})
	case 16:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				{Type: types.EventConnectionMetadataUpdated, ConnectionID: "direct-logical",
					Route: types.RouteDirect, Metadata: fixtureDirectMeta,
					Rule: "NETWORK,udp", Chains: []string{"DIRECT"}},
			}
		})
	case 17:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				{Type: types.EventConnectionDisappeared, ConnectionID: "missing-conn",
					PossibleUnobservedTail: true, Metadata: fixtureMissingMeta,
					Details: map[string]any{"lastObservedAt": fixtureBase().Add(13 * time.Second).Format(time.RFC3339Nano)}},
			}
		})
	case 19:
		emitEquivalenceFrame(t, s, frame, ts, func(f, e int64) []*types.CollectorEvent {
			return []*types.CollectorEvent{
				evNew("idle-zeros", 0, 0, 0, 0, types.RouteProxy, types.ClassKnownApplication, fixtureZeroMeta, "ZERO", []string{"NodeC"}),
			}
		})
	default:
		t.Fatalf("unexpected fixture frame %d", frame)
	}
}

func runEquivalenceDB(t *testing.T, dbPath string) (*sql.DB, func()) {
	t.Helper()
	db, err := OpenDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	return db, func() { _ = db.Close() }
}

// TestLegacyV2EquivalenceOnClosedDataset is the Phase 3S semantic gate: a
// legacy full rebuild and a v2 seed + incremental progression over the same
// closed deterministic dataset must produce identical accounting semantics.
func TestLegacyV2EquivalenceOnClosedDataset(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()

	// --- DB A: legacy full rebuild over the complete journal ---
	dbAPath, cleanupA := createAccountingTestDB(t)
	defer cleanupA()
	dbA, closeA := runEquivalenceDB(t, dbAPath)

	sinkA, err := OpenSQLiteSink(ctx, dbAPath, equivalenceFixtureSession, "v-eq")
	if err != nil {
		t.Fatalf("sink A: %v", err)
	}
	for frame := int64(1); frame <= 19; frame++ {
		emitEquivalenceFrameScript(t, sinkA, frame, base.Add(time.Duration(frame)*time.Second))
	}
	if _, err := RebuildAccounting(ctx, dbA, "legacy equivalence"); err != nil {
		t.Fatalf("legacy rebuild failed: %v", err)
	}

	// --- DB B: v2 seed on the prefix, then incremental over the remainder ---
	dbBPath, cleanupB := createAccountingTestDB(t)
	defer cleanupB()
	dbB, closeB := runEquivalenceDB(t, dbBPath)

	sinkB, err := OpenSQLiteSink(ctx, dbBPath, equivalenceFixtureSession, "v-eq")
	if err != nil {
		t.Fatalf("sink B: %v", err)
	}
	for frame := int64(1); frame <= 12; frame++ {
		emitEquivalenceFrameScript(t, sinkB, frame, base.Add(time.Duration(frame)*time.Second))
	}
	if _, err := AdvanceAccountingV2(ctx, dbB, "seed prefix", 0); err != nil {
		t.Fatalf("v2 seed failed: %v", err)
	}
	gen, err := GetActiveAccountingGeneration(ctx, dbB)
	if err != nil {
		t.Fatalf("expected active generation after seed: %v", err)
	}

	for frame := int64(13); frame <= 19; frame++ {
		emitEquivalenceFrameScript(t, sinkB, frame, base.Add(time.Duration(frame)*time.Second))
	}
	if _, err := AdvanceAccountingV2(ctx, dbB, "incremental", 0); err != nil {
		t.Fatalf("v2 incremental failed: %v", err)
	}
	gen, err = GetActiveAccountingGeneration(ctx, dbB)
	if err != nil {
		t.Fatalf("active generation missing: %v", err)
	}
	var journalMax int64
	if err := dbB.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&journalMax); err != nil {
		t.Fatal(err)
	}
	if gen.PublishedJournalSequence != journalMax {
		t.Fatalf("published boundary %d != journal max %d", gen.PublishedJournalSequence, journalMax)
	}
	fresh, err := NewAnalyticsService(dbB).GetAccountingFreshness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.IsFresh {
		t.Fatalf("v2 freshness must be fresh after publish: %+v", fresh)
	}

	// --- totals: raw and accounted byte sums ---
	var rawUpA, rawDownA, accUpA, accDownA int64
	if err := dbA.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(raw_upload),0), COALESCE(SUM(raw_download),0),
		       COALESCE(SUM(accounted_upload),0), COALESCE(SUM(accounted_download),0)
		FROM accounted_traffic;`).Scan(&rawUpA, &rawDownA, &accUpA, &accDownA); err != nil {
		t.Fatal(err)
	}
	var rawUpB, rawDownB, accUpB, accDownB int64
	if err := dbB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(raw_upload),0), COALESCE(SUM(raw_download),0),
		       COALESCE(SUM(accounted_upload),0), COALESCE(SUM(accounted_download),0)
		FROM accounted_traffic_v2;`).Scan(&rawUpB, &rawDownB, &accUpB, &accDownB); err != nil {
		t.Fatal(err)
	}
	if rawUpA != rawUpB || rawDownA != rawDownB || accUpA != accUpB || accDownA != accDownB {
		t.Fatalf("byte totals diverge: legacy raw %d/%d acc %d/%d vs v2 raw %d/%d acc %d/%d",
			rawUpA, rawDownA, accUpA, accDownA, rawUpB, rawDownB, accUpB, accDownB)
	}
	// Expected fixture semantics: candidate is confirmed (accounted 0),
	// logical keeps its deltas, missing keeps its bytes.
	if accUpA != 400+1000+300 || accDownA != 800+2000+400 {
		t.Fatalf("unexpected accounted totals %d/%d", accUpA, accDownA)
	}

	// --- per (route, class) accounted sums ---
	type keySums map[string][2]int64
	querySums := func(db *sql.DB, table, keyCol string) (keySums, keySums, error) {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`
			SELECT route, accounting_class, COALESCE(SUM(accounted_upload),0), COALESCE(SUM(accounted_download),0),
			       COALESCE(SUM(raw_upload),0), COALESCE(SUM(raw_download),0)
			FROM %s GROUP BY route, accounting_class;`, table))
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		acc := keySums{}
		raw := keySums{}
		for rows.Next() {
			var route, class string
			var up, down, rup, rdown int64
			if err := rows.Scan(&route, &class, &up, &down, &rup, &rdown); err != nil {
				return nil, nil, err
			}
			k := route + "|" + class
			acc[k] = [2]int64{up, down}
			raw[k] = [2]int64{rup, rdown}
		}
		return acc, raw, rows.Err()
	}
	accA, rawA, err := querySums(dbA, "accounted_traffic", "run_id")
	if err != nil {
		t.Fatal(err)
	}
	accB, rawB, err := querySums(dbB, "accounted_traffic_v2", "generation_id")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range accA {
		if accB[k] != v {
			t.Fatalf("route/class accounted sums diverge for %s: legacy %v v2 %v", k, v, accB[k])
		}
		if rawB[k] != rawA[k] {
			t.Fatalf("route/class raw sums diverge for %s: legacy %v v2 %v", k, rawA[k], rawB[k])
		}
	}
	if len(accA) != len(accB) {
		t.Fatalf("route/class key sets diverge: legacy %d keys, v2 %d keys", len(accA), len(accB))
	}

	// --- relay relations: same decisions ---
	type relation struct {
		cand    string
		logical string
		status  string
	}
	loadRelations := func(db *sql.DB, table string) ([]relation, error) {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`
			SELECT candidate_connection_id, COALESCE(logical_connection_id,''), status FROM %s;`, table))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []relation
		for rows.Next() {
			var r relation
			if err := rows.Scan(&r.cand, &r.logical, &r.status); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	relA, err := loadRelations(dbA, "relay_relations")
	if err != nil {
		t.Fatal(err)
	}
	relB, err := loadRelations(dbB, "relay_relations_v2")
	if err != nil {
		t.Fatal(err)
	}
	if len(relA) != 1 || len(relB) != 1 {
		t.Fatalf("expected exactly one relay relation, legacy %d v2 %d (%v / %v)", len(relA), len(relB), relA, relB)
	}
	if relA[0] != relB[0] || relA[0].cand != "cand-relay" || relA[0].status != string(RelayConfirmed) {
		t.Fatalf("relay decisions diverge: legacy %+v v2 %+v", relA, relB)
	}

	// --- hourly aggregates: byte fields exact; connection_count equal except
	// for the documented zero-byte-only contributor compaction ---
	loadHourly := func(db *sql.DB, table, keyCol string) (map[hourlyKey][8]int64, error) {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`
			SELECT bucket_start, dimension_type, dimension_key, route,
			       upload_bytes, download_bytes, connection_count,
			       exact_upload_bytes, exact_download_bytes, estimated_upload_bytes, estimated_download_bytes
			FROM %s;`, table))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[hourlyKey][8]int64{}
		for rows.Next() {
			var k hourlyKey
			var v [8]int64
			if err := rows.Scan(&k.bucket, &k.dimType, &k.dimKey, &k.route,
				&v[0], &v[1], &v[2], &v[3], &v[4], &v[5], &v[6]); err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, rows.Err()
	}
	hA, err := loadHourly(dbA, "usage_hourly_dimensions", "run_id")
	if err != nil {
		t.Fatal(err)
	}
	hB, err := loadHourly(dbB, "usage_hourly_dimensions_v2", "generation_id")
	if err != nil {
		t.Fatal(err)
	}
	for k, va := range hA {
		vb, ok := hB[k]
		allZeroBytes := va[0] == 0 && va[1] == 0 && va[3] == 0 && va[4] == 0 && va[5] == 0 && va[6] == 0
		if allZeroBytes && !ok {
			// Documented compaction: hourly keys whose entire legacy
			// contribution is zero-byte rows do not exist in v2.
			continue
		}
		if !ok {
			t.Fatalf("hourly key %v missing in v2", k)
		}
		if allZeroBytes {
			// A v2 key can legitimately remain with zero bytes after a
			// correction zeroed its contributions; only the connection
			// count rule below still applies.
			if vb[0] != 0 || vb[1] != 0 || vb[3] != 0 || vb[4] != 0 || vb[5] != 0 || vb[6] != 0 {
				t.Fatalf("v2 all-zero legacy key %v has nonzero bytes: %v", k, vb)
			}
		}
		if va[0] != vb[0] || va[1] != vb[1] || va[3] != vb[3] || va[4] != vb[4] || va[5] != vb[5] || va[6] != vb[6] {
			t.Fatalf("hourly bytes diverge for %v: legacy %v v2 %v", k, va, vb)
		}
		if va[2] != vb[2] {
			// The only allowed divergence: legacy counts connections whose
			// entire contribution to this key is zero-byte rows (documented
			// derived compaction). The gap must be exactly explained.
			zero, zerr := zeroByteOnlyCount(ctx, dbA, k)
			if zerr != nil {
				t.Fatal(zerr)
			}
			if va[2]-vb[2] != zero {
				t.Fatalf("hourly connection_count gap unexplained for %v: legacy %d v2 %d, zero-byte-only=%d",
					k, va[2], vb[2], zero)
			}
		}
	}
	for k := range hB {
		if _, ok := hA[k]; !ok {
			t.Fatalf("v2 hourly key %v absent from legacy", k)
		}
	}

	// --- usage summary (route totals) through the analytics layer ---
	sumA, err := NewAnalyticsService(dbA).GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	sumB, err := NewAnalyticsService(dbB).GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if sumA.ProxyUpload != sumB.ProxyUpload || sumA.ProxyDownload != sumB.ProxyDownload ||
		sumA.DirectUpload != sumB.DirectDownload || sumA.DirectDownload != sumB.DirectDownload ||
		sumA.UnknownRouteUpload != sumB.UnknownRouteUpload || sumA.MissingAttributionUpload != sumB.MissingAttributionUpload ||
		sumA.UniqueObservedUpload != sumB.UniqueObservedUpload || sumA.UniqueObservedDownload != sumB.UniqueObservedDownload {
		t.Fatalf("usage summaries diverge:\nlegacy: %+v\nv2:     %+v", sumA, sumB)
	}

	_ = closeA
	_ = closeB
}

// zeroByteOnlyCount counts how many distinct connections contribute to the
// legacy dimension key exclusively through zero-byte accounted rows - exactly
// the population the documented v2 compaction omits from connection_count.
// The fixture lives in a single hour bucket, so bucket scoping is implicit.
func zeroByteOnlyCount(ctx context.Context, db *sql.DB, k hourlyKey) (int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, epoch_id, connection_id, accounted_upload, accounted_download,
		       process, host, destination_ip, network, rule, rule_payload, final_proxy, top_policy_group, route,
		       accounting_class
		FROM accounted_traffic;
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type connAgg struct {
		nonzero map[string]bool
		any     map[string]bool
	}
	aggs := map[string]*connAgg{}
	for rows.Next() {
		var sess, connID string
		var epoch int
		var accUp, accDown int64
		var process, host, destIP, network, rule, rulePayload, finalProxy, topGroup, route, class sql.NullString
		if err := rows.Scan(&sess, &epoch, &connID, &accUp, &accDown,
			&process, &host, &destIP, &network, &rule, &rulePayload, &finalProxy, &topGroup, &route, &class); err != nil {
			return 0, err
		}
		dims := accountedDimensionPairs(process, host, destIP, network, rule, rulePayload, finalProxy, topGroup)
		ck := fmt.Sprintf("%s:%d:%s", sess, epoch, connID)
		a := aggs[ck]
		if a == nil {
			a = &connAgg{nonzero: map[string]bool{}, any: map[string]bool{}}
			aggs[ck] = a
		}
		// Legacy excludes confirmed duplicates from distinct connection
		// counting entirely; mirror that here.
		if !class.Valid || class.String == "confirmed_relay_duplicate" {
			continue
		}
		for _, d := range dims {
			target := d.dimType + "|" + d.dimKey + "|" + route.String
			if accUp != 0 || accDown != 0 {
				a.nonzero[target] = true
			}
			a.any[target] = true
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	count := int64(0)
	target := k.dimType + "|" + k.dimKey + "|" + k.route
	for _, a := range aggs {
		if a.any[target] && !a.nonzero[target] {
			count++
		}
	}
	return count, nil
}
