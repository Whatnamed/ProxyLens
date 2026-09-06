package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// bulkInsertJournalRows appends n synthetic nonzero ConnectionDelta journal
// rows directly into event_journal. Accounting v2 reads only the journal, so
// this builds dense history volume quickly without per-event projection work.
func bulkInsertJournalRows(t *testing.T, db *sql.DB, n int) error {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var maxSeq int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(journal_sequence), 0) FROM event_journal;`).Scan(&maxSeq); err != nil {
		return err
	}
	base := fixtureBase()
	stmt, err := tx.Prepare(`
		INSERT INTO event_journal (
			event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type,
			observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
		) VALUES (?, ?, 1, ?, 1, 'ConnectionDelta', ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	const conns = 100
	for i := 0; i < n; i++ {
		seq := maxSeq + int64(i) + 1
		obsTs := base.Add(time.Duration(50+i%1000) * time.Second).UTC().Format(time.RFC3339Nano)
		connID := fmt.Sprintf("bulk-%d", i%conns)
		eventJSON := fmt.Sprintf(
			`{"eventId":"bulk-%d","sessionId":"%s","epochId":1,"frameSequence":%d,"eventSequence":1,`+
				`"timestamp":%q,"type":"ConnectionDelta","connectionId":%q,`+
				`"observedUploadCounter":%d,"observedDownloadCounter":%d,`+
				`"deltaUpload":100,"deltaDownload":200,`+
				`"monitoredCumulativeUpload":%d,"monitoredCumulativeDownload":%d,`+
				`"route":"PROXY","attributionClass":"known_application",`+
				`"metadata":{"network":"tcp","process":"app.exe","host":"logical.example"},`+
				`"rule":"MATCH","chains":["NodeA","GroupX"]}`,
			i, equivalenceFixtureSession, 100000+i, obsTs, connID,
			1000+100*i, 2000+200*i, 1000+100*i, 2000+200*i)
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(eventJSON)))
		if _, err := stmt.Exec(
			fmt.Sprintf("bulkje-%d", i), equivalenceFixtureSession, 100000+i,
			obsTs, connID, eventJSON, sum, obsTs, seq,
		); err != nil {
			return err
		}
		if i%10000 == 9999 {
			if err := tx.Commit(); err != nil {
				return err
			}
			tx, err = db.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()
			stmt, err = tx.Prepare(`
				INSERT INTO event_journal (
					event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type,
					observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
				) VALUES (?, ?, 1, ?, 1, 'ConnectionDelta', ?, ?, ?, ?, ?, ?);
			`)
			if err != nil {
				return err
			}
			defer stmt.Close()
		}
	}
	return tx.Commit()
}

func emitZeroSamplingResidual(t *testing.T, s *SQLiteEventSink, frameSequence, eventSequence int64, ts time.Time) {
	t.Helper()
	residual := &types.CollectorEvent{
		Type:          types.EventSamplingResidual,
		SessionID:     equivalenceFixtureSession,
		EpochID:       1,
		FrameSequence: frameSequence,
		EventSequence: eventSequence,
		Timestamp:     ts,
		Details: map[string]any{
			"residualUpload":   int64(0),
			"residualDownload": int64(0),
		},
	}
	residual.GenerateDeterministicEventID()
	if err := s.Emit(residual); err != nil {
		t.Fatalf("emit sampling residual for frame %d failed: %v", frameSequence, err)
	}
}

// TestIncrementalPublishBoundaryAndRuns proves the publish contract: each
// incremental run records its exact (from, to] range, the published boundary
// only advances to fully derived ranges, and a no-evidence tick is a no-op.
func TestIncrementalPublishBoundaryAndRuns(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()
	db, closeDB := runEquivalenceDB(t, dbPath)
	defer closeDB()

	s, err := OpenSQLiteSink(ctx, dbPath, equivalenceFixtureSession, "v-eq")
	if err != nil {
		t.Fatal(err)
	}
	for frame := int64(1); frame <= 12; frame++ {
		emitEquivalenceFrameScript(t, s, frame, base.Add(time.Duration(frame)*time.Second))
	}
	if _, err := AdvanceAccountingV2(ctx, db, "seed", 0); err != nil {
		t.Fatal(err)
	}
	gen, err := GetActiveAccountingGeneration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	seedBoundary := gen.PublishedJournalSequence
	if seedBoundary == 0 {
		t.Fatal("seed boundary must be positive")
	}

	for frame := int64(13); frame <= 19; frame++ {
		emitEquivalenceFrameScript(t, s, frame, base.Add(time.Duration(frame)*time.Second))
	}
	var eventsInRange int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE journal_sequence > ?;`, seedBoundary).Scan(&eventsInRange); err != nil {
		t.Fatal(err)
	}
	if _, err := AdvanceAccountingV2(ctx, db, "incremental", 0); err != nil {
		t.Fatal(err)
	}
	var fromSeq, toSeq, processed int64
	var mode string
	if err := db.QueryRowContext(ctx, `
		SELECT from_sequence_exclusive, to_sequence_inclusive, processed_events, mode
		FROM accounting_runs_v2 WHERE mode = 'incremental' ORDER BY started_at DESC LIMIT 1;
	`).Scan(&fromSeq, &toSeq, &processed, &mode); err != nil {
		t.Fatal(err)
	}
	if fromSeq != seedBoundary {
		t.Fatalf("incremental range must start at the published boundary %d, got %d", seedBoundary, fromSeq)
	}
	if processed != eventsInRange {
		t.Fatalf("processed_events %d != range event count %d", processed, eventsInRange)
	}
	gen, err = GetActiveAccountingGeneration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if gen.PublishedJournalSequence != toSeq {
		t.Fatalf("published %d must equal run boundary %d", gen.PublishedJournalSequence, toSeq)
	}

	// A no-evidence tick must not advance anything.
	before := gen.PublishedJournalSequence
	rec, err := AdvanceAccountingV2(ctx, db, "idle tick", 0)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("no-evidence tick must return a run record, not nil")
	}
	gen, _ = GetActiveAccountingGeneration(ctx, db)
	if gen.PublishedJournalSequence != before {
		t.Fatalf("idle tick advanced the boundary: %d -> %d", before, gen.PublishedJournalSequence)
	}
}

// TestIncrementalCancelLeavesNoGarbage proves cancellation safety: a canceled
// incremental call changes nothing (single-transaction publish), and an
// immediate retry produces the same totals as an uninterrupted run.
func TestIncrementalCancelLeavesNoGarbage(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()

	build := func(t *testing.T, withCancel bool) [4]int64 {
		dbPath, cleanup := createAccountingTestDB(t)
		defer cleanup()
		db, closeDB := runEquivalenceDB(t, dbPath)
		defer closeDB()
		s, err := OpenSQLiteSink(ctx, dbPath, equivalenceFixtureSession, "v-eq")
		if err != nil {
			t.Fatal(err)
		}
		for frame := int64(1); frame <= 12; frame++ {
			emitEquivalenceFrameScript(t, s, frame, base.Add(time.Duration(frame)*time.Second))
		}
		if _, err := AdvanceAccountingV2(ctx, db, "seed", 0); err != nil {
			t.Fatal(err)
		}
		gen, err := GetActiveAccountingGeneration(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		published0 := gen.PublishedJournalSequence

		for frame := int64(13); frame <= 19; frame++ {
			emitEquivalenceFrameScript(t, s, frame, base.Add(time.Duration(frame)*time.Second))
		}

		if withCancel {
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := AdvanceAccountingV2(cancelCtx, db, "canceled", 0); err == nil {
				t.Fatal("canceled advance must fail")
			}
			gen, err = GetActiveAccountingGeneration(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if gen.PublishedJournalSequence != published0 {
				t.Fatalf("canceled advance moved the boundary: %d -> %d", published0, gen.PublishedJournalSequence)
			}
			var v2Rows int
			if err := db.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM accounted_traffic_v2 WHERE source_journal_sequence > ?;
			`, published0).Scan(&v2Rows); err != nil {
				t.Fatal(err)
			}
			if v2Rows != 0 {
				t.Fatalf("canceled advance left %d unpublished derived rows", v2Rows)
			}
		}

		if _, err := AdvanceAccountingV2(ctx, db, "final", 0); err != nil {
			t.Fatal(err)
		}
		var totals [4]int64
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(raw_upload),0), COALESCE(SUM(raw_download),0),
			       COALESCE(SUM(accounted_upload),0), COALESCE(SUM(accounted_download),0)
			FROM accounted_traffic_v2;`).Scan(&totals[0], &totals[1], &totals[2], &totals[3]); err != nil {
			t.Fatal(err)
		}
		return totals
	}

	clean := build(t, false)
	canceled := build(t, true)
	if clean != canceled {
		t.Fatalf("canceled-then-retried totals diverge from clean run: clean %v canceled %v", clean, canceled)
	}
}

// TestSeedChunkedResumeMatchesSingleShot proves the seed is resumable: a tiny
// chunk budget spans many ticks and the final activated state is identical to
// a single-shot seed.
func TestSeedChunkedResumeMatchesSingleShot(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()

	run := func(t *testing.T, maxEvents int64) [4]int64 {
		dbPath, cleanup := createAccountingTestDB(t)
		defer cleanup()
		db, closeDB := runEquivalenceDB(t, dbPath)
		defer closeDB()
		s, err := OpenSQLiteSink(ctx, dbPath, equivalenceFixtureSession, "v-eq")
		if err != nil {
			t.Fatal(err)
		}
		for frame := int64(1); frame <= 19; frame++ {
			emitEquivalenceFrameScript(t, s, frame, base.Add(time.Duration(frame)*time.Second))
		}
		for {
			gen, err := GetActiveAccountingGeneration(ctx, db)
			if err == nil && gen != nil {
				break
			}
			if _, err := AdvanceAccountingV2(ctx, db, "chunked seed", maxEvents); err != nil {
				t.Fatal(err)
			}
		}
		var totals [4]int64
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(raw_upload),0), COALESCE(SUM(raw_download),0),
			       COALESCE(SUM(accounted_upload),0), COALESCE(SUM(accounted_download),0)
			FROM accounted_traffic_v2;`).Scan(&totals[0], &totals[1], &totals[2], &totals[3]); err != nil {
			t.Fatal(err)
		}
		return totals
	}

	single := run(t, 0)
	chunked := run(t, 2)
	if single != chunked {
		t.Fatalf("chunked seed diverged from single-shot seed: single %v chunked %v", single, chunked)
	}
}

type incrementalCostTiming struct {
	historyEvents     int
	setup             time.Duration
	baseFixture       time.Duration
	historyInsertion  time.Duration
	seed              time.Duration
	appendedInsertion time.Duration
	incremental       time.Duration
	freshness         time.Duration
	seedCalls         int
	seedRunRows       int64
	seedProcessed     int64
	seedBoundary      int64
}

func latestSeedProgress(ctx context.Context, db *sql.DB) (runRows, processed, boundary int64, err error) {
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(processed_events), 0),
		       COALESCE(MAX(to_sequence_inclusive), 0)
		FROM accounting_runs_v2
		WHERE mode = 'seed';
	`).Scan(&runRows, &processed, &boundary)
	return
}

func generationProgress(ctx context.Context, db *sql.DB) (status string, seedLast, materializeLast, published int64, err error) {
	gen, activeErr := GetActiveAccountingGeneration(ctx, db)
	if activeErr == nil && gen != nil {
		return gen.Status, gen.SeedLastSequence, gen.MaterializeLastSequence, gen.PublishedJournalSequence, nil
	}
	gen, err = getResumableGeneration(ctx, db)
	if err != nil {
		return "", 0, 0, 0, err
	}
	if gen == nil {
		return "none", 0, 0, 0, nil
	}
	return gen.Status, gen.SeedLastSequence, gen.MaterializeLastSequence, gen.PublishedJournalSequence, nil
}

// TestIncrementalConstantCost is the architectural guard proving incremental
// cost depends on the appended range, not on history size: the same appended
// batch on a large-history generation must not be orders of magnitude slower
// than on a small-history generation. The ordinary regression guard uses
// measured developer-scale histories; the full 1.5M-row proof belongs to the
// E-drive scale acceptance tool.
func TestIncrementalConstantCost(t *testing.T) {
	if testing.Short() {
		t.Skip("constant-cost test skipped in short mode")
	}
	ctx := context.Background()
	base := fixtureBase()

	buildAndMeasure := func(t *testing.T, historyEvents int) incrementalCostTiming {
		t.Helper()
		measurement := incrementalCostTiming{historyEvents: historyEvents}
		stageStart := time.Now()
		dbPath, cleanup := createAccountingTestDB(t)
		defer cleanup()
		db, closeDB := runEquivalenceDB(t, dbPath)
		defer closeDB()

		s, err := OpenSQLiteSink(ctx, dbPath, equivalenceFixtureSession, "v-eq")
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := s.Close(); err != nil {
				t.Errorf("close fixture sink: %v", err)
			}
		}()
		measurement.setup = time.Since(stageStart)

		stageStart = time.Now()
		emitEquivalenceFrameScript(t, s, 1, base.Add(time.Second))
		measurement.baseFixture = time.Since(stageStart)

		stageStart = time.Now()
		if err := bulkInsertJournalRows(t, db, historyEvents); err != nil {
			t.Fatal(err)
		}
		bulkFinalFrame := int64(100000 + historyEvents - 1)
		bulkFinalTimestamp := base.Add(time.Duration(50+(historyEvents-1)%1000) * time.Second)
		emitZeroSamplingResidual(t, s, bulkFinalFrame, 2, bulkFinalTimestamp)
		measurement.historyInsertion = time.Since(stageStart)

		// Seed to activation (may span multiple chunked calls).
		stageStart = time.Now()
		for {
			gen, genErr := GetActiveAccountingGeneration(ctx, db)
			if genErr == nil && gen != nil {
				break
			}
			callStart := time.Now()
			if _, err := AdvanceAccountingV2(ctx, db, "seed", 0); err != nil {
				t.Fatal(err)
			}
			measurement.seedCalls++
			if measurement.seedCalls > 1000 {
				t.Fatalf("seed did not activate after %d calls; fixture likely contains an incomplete final frame", measurement.seedCalls)
			}
			runRows, processed, boundary, err := latestSeedProgress(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			measurement.seedRunRows = runRows
			measurement.seedProcessed = processed
			measurement.seedBoundary = boundary
			status, seedLast, materializeLast, published, err := generationProgress(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("history=%d seed call=%d callDuration=%v seedRuns=%d processed=%d maxBoundary=%d generation=%s seedLast=%d materializeLast=%d published=%d",
				historyEvents, measurement.seedCalls, time.Since(callStart), runRows, processed, boundary,
				status, seedLast, materializeLast, published)
		}
		measurement.seed = time.Since(stageStart)

		// Identical appended batch for both histories, inserted directly into
		// the journal beyond the published boundary.
		stageStart = time.Now()
		var maxSeq int64
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&maxSeq); err != nil {
			t.Fatal(err)
		}
		const appended = 2000
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO event_journal (
				event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type,
				observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
			) VALUES (?, ?, 1, ?, 1, 'ConnectionDelta', ?, ?, ?, ?, ?, ?);
		`)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < appended; i++ {
			seq := maxSeq + int64(i) + 1
			obsTs := base.Add(time.Duration(5000+i) * time.Second).UTC().Format(time.RFC3339Nano)
			connID := fmt.Sprintf("appended-%d", i%50)
			eventJSON := fmt.Sprintf(
				`{"eventId":"appended-%d-%d","sessionId":"%s","epochId":1,"frameSequence":%d,"eventSequence":1,`+
					`"timestamp":%q,"type":"ConnectionDelta","connectionId":%q,`+
					`"observedUploadCounter":%d,"observedDownloadCounter":%d,`+
					`"deltaUpload":100,"deltaDownload":200,`+
					`"monitoredCumulativeUpload":%d,"monitoredCumulativeDownload":%d,`+
					`"route":"PROXY","attributionClass":"known_application",`+
					`"metadata":{"network":"tcp","process":"app.exe","host":"logical.example"},`+
					`"rule":"MATCH","chains":["NodeA","GroupX"]}`,
				historyEvents, i, equivalenceFixtureSession, 900000+i, obsTs, connID,
				1000+100*i, 2000+200*i, 1000+100*i, 2000+200*i)
			sum := fmt.Sprintf("%x", sha256.Sum256([]byte(eventJSON)))
			if _, err := stmt.ExecContext(ctx,
				fmt.Sprintf("appended-%d-%d", historyEvents, i), equivalenceFixtureSession, 900000+i,
				obsTs, connID, eventJSON, sum, obsTs, seq,
			); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		stmt.Close()
		appendedFinalFrame := int64(900000 + appended - 1)
		appendedFinalTimestamp := base.Add(time.Duration(5000+appended-1) * time.Second)
		emitZeroSamplingResidual(t, s, appendedFinalFrame, 2, appendedFinalTimestamp)
		measurement.appendedInsertion = time.Since(stageStart)

		stageStart = time.Now()
		if _, err := AdvanceAccountingV2(ctx, db, "appended batch", 0); err != nil {
			t.Fatal(err)
		}
		measurement.incremental = time.Since(stageStart)

		stageStart = time.Now()
		fresh, err := NewAnalyticsService(db).GetAccountingFreshness(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !fresh.IsFresh {
			t.Fatalf("history %d: expected fresh after incremental, got %+v", historyEvents, fresh)
		}
		measurement.freshness = time.Since(stageStart)
		t.Logf("history=%d timing setup=%v baseFixture=%v historyInsertion=%v seed=%v appendedInsertion=%v incremental=%v freshness=%v seedCalls=%d seedRuns=%d seedProcessed=%d seedBoundary=%d",
			historyEvents, measurement.setup, measurement.baseFixture, measurement.historyInsertion,
			measurement.seed, measurement.appendedInsertion, measurement.incremental, measurement.freshness,
			measurement.seedCalls, measurement.seedRunRows, measurement.seedProcessed, measurement.seedBoundary)
		return measurement
	}

	const (
		smallHistoryEvents     = 10000
		largeHistoryEvents     = 300000
		maxIncrementalSlowdown = 20.0
	)
	// The ordinary guard uses a 30x history spread while keeping the test in
	// the tens-of-seconds range on a developer machine. A 20x ceiling rejects
	// an approximately history-linear regression without making this test a
	// substitute for the 1.5M-row E-drive scale acceptance.
	measurements := make([]incrementalCostTiming, 0, 2)
	for _, historyEvents := range []int{smallHistoryEvents, largeHistoryEvents} {
		historyEvents := historyEvents
		t.Run(fmt.Sprintf("history_%dk", historyEvents/1000), func(t *testing.T) {
			measurements = append(measurements, buildAndMeasure(t, historyEvents))
		})
	}
	if len(measurements) != 2 {
		t.Fatalf("constant-cost comparison requires both history fixtures; completed %d", len(measurements))
	}

	small := measurements[0]
	large := measurements[len(measurements)-1]
	ratio := float64(large.incremental) / float64(small.incremental)
	t.Logf("incremental duration: small-history(%d)=%v large-history(%d)=%v ratio=%.1fx",
		small.historyEvents, small.incremental, large.historyEvents, large.incremental, ratio)
	if ratio >= maxIncrementalSlowdown {
		t.Fatalf("large history made a small incremental batch %.0fx slower (small=%v large=%v)", ratio, small.incremental, large.incremental)
	}
}

// TestIncrementalRelayMatchViaHistoricalClosure proves the historical closure
// contract: a previously seeded, still-active connection A (silent in the
// current chunk) is paired with a newly dirty connection B via closure
// loading, without requiring A to be dirty in this chunk.
func TestIncrementalRelayMatchViaHistoricalClosure(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()
	db, closeDB := runEquivalenceDB(t, dbPath)
	defer closeDB()

	const sess = "sess-closure-match"
	s, err := OpenSQLiteSink(ctx, dbPath, sess, "v-closure")
	if err != nil {
		t.Fatal(err)
	}

	proxyChains := []string{"NodeA", "GroupX"}

	// Frame 1: Seed candidate A alone.
	ts1 := base.Add(time.Second)
	emitEquivalenceFrame(t, s, 1, ts1, func(f, e int64) []*types.CollectorEvent {
		return []*types.CollectorEvent{
			evNew("cand-A", 1000, 2000, 1000, 2000, types.RouteProxy, types.ClassRelayCandidate, fixtureCandMeta, "", proxyChains),
		}
	})

	// Seed the generation up to frame 1.
	if _, err := AdvanceAccountingV2(ctx, db, "seed A", 0); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Verify candidate A is initial missing attribution in state table.
	var classA string
	if err := db.QueryRowContext(ctx, `
		SELECT accounting_class FROM accounting_conn_state_v2
		WHERE connection_id = 'cand-A';
	`).Scan(&classA); err != nil {
		t.Fatalf("failed to query cand-A class: %v", err)
	}
	if classA != string(ClassMissingAttribution) {
		t.Fatalf("expected cand-A to start as missing_attribution, got %s", classA)
	}

	// Frame 2: Incremental chunk with logical B ONLY.
	// Candidate A is completely silent (no delta, no presence, no events).
	ts2 := base.Add(2 * time.Second)
	emitEquivalenceFrame(t, s, 2, ts2, func(f, e int64) []*types.CollectorEvent {
		return []*types.CollectorEvent{
			evNew("log-B", 1000, 2000, 1000, 2000, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
		}
	})

	// Run incremental accounting.
	if _, err := AdvanceAccountingV2(ctx, db, "incremental B", 0); err != nil {
		t.Fatalf("incremental failed: %v", err)
	}

	// A must now be reclassified as confirmed_relay_duplicate via historical closure.
	if err := db.QueryRowContext(ctx, `
		SELECT accounting_class FROM accounting_conn_state_v2
		WHERE connection_id = 'cand-A';
	`).Scan(&classA); err != nil {
		t.Fatalf("failed to query updated cand-A class: %v", err)
	}
	if classA != string(ClassConfirmedRelayDuplicate) {
		t.Fatalf("cand-A should have been reclassified as confirmed_relay_duplicate, got %s", classA)
	}

	// A relation must be recorded in relay_relations_v2 pairing cand-A to log-B.
	var relLogID, relStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT logical_connection_id, status FROM relay_relations_v2
		WHERE candidate_connection_id = 'cand-A';
	`).Scan(&relLogID, &relStatus); err != nil {
		t.Fatalf("expected relay relation for cand-A: %v", err)
	}
	if relLogID != "log-B" || relStatus != "confirmed" {
		t.Fatalf("unexpected relay relation: logical=%s status=%s", relLogID, relStatus)
	}
}

// TestIncrementalStaleRelayRelationClearedOnClassChange verifies that when a
// candidate connection X becomes logical/unique in a later incremental chunk
// (e.g. via MetadataUpdated), its prior row in relay_relations_v2 is cleared
// and historical accounted bytes are corrected back to unique.
func TestIncrementalStaleRelayRelationClearedOnClassChange(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()
	db, closeDB := runEquivalenceDB(t, dbPath)
	defer closeDB()

	const sess = "sess-stale-relation"
	s, err := OpenSQLiteSink(ctx, dbPath, sess, "v-stale")
	if err != nil {
		t.Fatal(err)
	}

	proxyChains := []string{"NodeA", "GroupX"}

	// Frame 1: Candidate X and Logical L appear together.
	ts1 := base.Add(time.Second)
	emitEquivalenceFrame(t, s, 1, ts1, func(f, e int64) []*types.CollectorEvent {
		return []*types.CollectorEvent{
			evNew("cand-X", 1000, 2000, 1000, 2000, types.RouteProxy, types.ClassRelayCandidate, fixtureCandMeta, "", proxyChains),
			evNew("log-L", 1000, 2000, 1000, 2000, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
		}
	})

	// Seed initial generation.
	if _, err := AdvanceAccountingV2(ctx, db, "seed X and L", 0); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Candidate X must be classified as confirmed_relay_duplicate.
	var classX string
	if err := db.QueryRowContext(ctx, `SELECT accounting_class FROM accounting_conn_state_v2 WHERE connection_id = 'cand-X';`).Scan(&classX); err != nil {
		t.Fatal(err)
	}
	if classX != string(ClassConfirmedRelayDuplicate) {
		t.Fatalf("expected cand-X to be confirmed_relay_duplicate initially, got %s", classX)
	}

	// relay_relations_v2 must have an entry for cand-X.
	var relCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM relay_relations_v2 WHERE candidate_connection_id = 'cand-X';`).Scan(&relCount); err != nil {
		t.Fatal(err)
	}
	if relCount != 1 {
		t.Fatalf("expected 1 relay relation for cand-X, got %d", relCount)
	}

	// Record generation's published accounted totals before the change.
	var genBefore AccountingGeneration
	if err := db.QueryRowContext(ctx, `
		SELECT published_accounted_upload, published_accounted_download
		FROM accounting_generations WHERE status = 'active';
	`).Scan(&genBefore.PublishedAccountedUpload, &genBefore.PublishedAccountedDownload); err != nil {
		t.Fatal(err)
	}
	// Because cand-X was confirmed duplicate, only log-L (1000 / 2000) was accounted.
	if genBefore.PublishedAccountedUpload != 1000 || genBefore.PublishedAccountedDownload != 2000 {
		t.Fatalf("unexpected accounted totals before change: up=%d down=%d",
			genBefore.PublishedAccountedUpload, genBefore.PublishedAccountedDownload)
	}

	// Frame 2: cand-X receives MetadataUpdated providing process + rule,
	// promoting it to an independent logical connection (unique).
	ts2 := base.Add(2 * time.Second)
	emitEquivalenceFrame(t, s, 2, ts2, func(f, e int64) []*types.CollectorEvent {
		return []*types.CollectorEvent{
			{
				Type:         types.EventConnectionMetadataUpdated,
				Timestamp:    ts2,
				ConnectionID: "cand-X",
				Metadata: types.RawMetadata{
					Network: "tcp",
					Process: "promoted.exe",
					Host:    "promoted.example.com",
				},
				Rule:        "DOMAIN-KEYWORD,promoted",
				RulePayload: "promoted",
				Chains:      proxyChains,
				Route:       types.RouteProxy,
			},
		}
	})

	// Run incremental accounting chunk.
	if _, err := AdvanceAccountingV2(ctx, db, "incremental promote X", 0); err != nil {
		t.Fatalf("incremental chunk failed: %v", err)
	}

	// cand-X must now be unique.
	if err := db.QueryRowContext(ctx, `SELECT accounting_class FROM accounting_conn_state_v2 WHERE connection_id = 'cand-X';`).Scan(&classX); err != nil {
		t.Fatal(err)
	}
	if classX != string(ClassUnique) {
		t.Fatalf("expected cand-X to become unique after MetadataUpdated, got %s", classX)
	}

	// The stale relay_relations_v2 entry for cand-X MUST be gone.
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM relay_relations_v2 WHERE candidate_connection_id = 'cand-X';`).Scan(&relCount); err != nil {
		t.Fatal(err)
	}
	if relCount != 0 {
		t.Fatalf("expected stale relay_relations_v2 entry for cand-X to be deleted, got %d rows", relCount)
	}

	// Historical accounted totals must now include cand-X's 1000/2000 bytes.
	var genAfter AccountingGeneration
	if err := db.QueryRowContext(ctx, `
		SELECT published_accounted_upload, published_accounted_download
		FROM accounting_generations WHERE status = 'active';
	`).Scan(&genAfter.PublishedAccountedUpload, &genAfter.PublishedAccountedDownload); err != nil {
		t.Fatal(err)
	}
	if genAfter.PublishedAccountedUpload != 2000 || genAfter.PublishedAccountedDownload != 4000 {
		t.Fatalf("expected accounted totals to be corrected to 2000/4000, got up=%d down=%d",
			genAfter.PublishedAccountedUpload, genAfter.PublishedAccountedDownload)
	}
}

// TestIncrementalBoundaryRefusesIncompleteFrame verifies that when an in-progress
// frame has committed some events but its completion evidence (SamplingResidual)
// has not yet committed, incremental boundary selection refuses to advance into
// the middle of the incomplete frame. Once the frame completes, the boundary
// advances to cover the whole frame.
func TestIncrementalBoundaryRefusesIncompleteFrame(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()
	db, closeDB := runEquivalenceDB(t, dbPath)
	defer closeDB()

	sess := equivalenceFixtureSession
	s, err := OpenSQLiteSink(ctx, dbPath, sess, "v-boundary")
	if err != nil {
		t.Fatal(err)
	}

	proxyChains := []string{"NodeA", "GroupX"}

	// Frame 1: Complete frame with ConnectionBootstrap and SamplingResidual.
	ts1 := base.Add(time.Second)
	emitEquivalenceFrame(t, s, 1, ts1, func(f, e int64) []*types.CollectorEvent {
		return []*types.CollectorEvent{
			evNew("conn-1", 100, 200, 100, 200, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
			{
				Type:      types.EventSamplingResidual,
				Timestamp: ts1,
				Details: map[string]any{
					"residualUpload":   int64(0),
					"residualDownload": int64(0),
				},
			},
		}
	})

	// Seed initial generation to publish frame 1.
	for {
		gen, genErr := GetActiveAccountingGeneration(ctx, db)
		if genErr == nil && gen != nil {
			break
		}
		if _, err := AdvanceAccountingV2(ctx, db, "seed frame 1", 0); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	gen, err := GetActiveAccountingGeneration(ctx, db)
	if err != nil || gen == nil {
		t.Fatalf("expected active generation, got gen=%v err=%v", gen, err)
	}
	p1 := gen.PublishedJournalSequence
	if p1 <= 0 {
		t.Fatalf("expected positive published sequence, got %d", p1)
	}

	// Frame 2: Partially emitted frame.
	// Commit a ConnectionDelta event, but DO NOT commit the SamplingResidual yet.
	ts2 := base.Add(2 * time.Second)
	evIncomplete := &types.CollectorEvent{
		EventID:                 "ev-f2-delta",
		SessionID:               sess,
		EpochID:                 1,
		FrameSequence:           2,
		EventSequence:           1,
		Type:                    types.EventConnectionDelta,
		Timestamp:               ts2,
		ConnectionID:            "conn-1",
		ObservedUploadCounter:   200,
		ObservedDownloadCounter: 400,
		DeltaUpload:             100,
		DeltaDownload:           200,
		Route:                   types.RouteProxy,
		AttributionClass:        types.ClassKnownApplication,
		Metadata:                fixtureProxyMeta,
		Rule:                    "MATCH",
		Chains:                  proxyChains,
	}
	if err := s.Emit(evIncomplete); err != nil {
		t.Fatalf("failed to emit incomplete frame event: %v", err)
	}

	// 1. Calling frameAlignedIncrementalCut must refuse to advance into Frame 2!
	cutTo, currentMax, err := frameAlignedIncrementalCut(ctx, db, p1, 50000)
	if err != nil {
		t.Fatalf("frameAlignedIncrementalCut failed: %v", err)
	}
	if currentMax <= p1 {
		t.Fatalf("expected currentMax (%d) > p1 (%d)", currentMax, p1)
	}
	if cutTo != p1 {
		t.Fatalf("cutTo advanced into incomplete Frame 2: got %d, want %d (p1)", cutTo, p1)
	}

	// 2. AdvanceAccountingV2 must report a no-op (no new evidence) and NOT advance published boundary.
	run, err := AdvanceAccountingV2(ctx, db, "tick during incomplete frame", 0)
	if err != nil {
		t.Fatalf("AdvanceAccountingV2 failed: %v", err)
	}
	if *run.SourceJournalSequenceMax != p1 {
		t.Fatalf("published boundary advanced into incomplete frame: got %d, want %d",
			*run.SourceJournalSequenceMax, p1)
	}

	// 3. Now complete Frame 2 by emitting its final SamplingResidual evidence.
	evResidual := &types.CollectorEvent{
		EventID:       "ev-f2-residual",
		SessionID:     sess,
		EpochID:       1,
		FrameSequence: 2,
		EventSequence: 2,
		Type:          types.EventSamplingResidual,
		Timestamp:     ts2,
		Details: map[string]any{
			"globalUploadDelta":      int64(100),
			"globalDownloadDelta":    int64(200),
			"uniqueObservedUpload":   int64(100),
			"uniqueObservedDownload": int64(200),
			"residualUpload":         int64(0),
			"residualDownload":       int64(0),
		},
	}
	if err := s.Emit(evResidual); err != nil {
		t.Fatalf("failed to emit completion evidence: %v", err)
	}

	// 4. Now frameAlignedIncrementalCut must advance to the complete Frame 2 end!
	var f2MaxSeq int64
	if err := db.QueryRowContext(ctx, `
		SELECT MAX(journal_sequence) FROM event_journal WHERE session_id = ? AND frame_sequence = 2;
	`, sess).Scan(&f2MaxSeq); err != nil {
		t.Fatal(err)
	}

	cutTo, currentMax, err = frameAlignedIncrementalCut(ctx, db, p1, 50000)
	if err != nil {
		t.Fatalf("frameAlignedIncrementalCut after completion failed: %v", err)
	}
	if cutTo != f2MaxSeq {
		t.Fatalf("cutTo after completion did not reach frame end: got %d, want %d", cutTo, f2MaxSeq)
	}

	// 5. AdvanceAccountingV2 must successfully advance the boundary to f2MaxSeq!
	run, err = AdvanceAccountingV2(ctx, db, "tick after frame completed", 0)
	if err != nil {
		t.Fatalf("AdvanceAccountingV2 failed after completion: %v", err)
	}
	if *run.SourceJournalSequenceMax != f2MaxSeq {
		t.Fatalf("expected boundary to advance to %d, got %d", f2MaxSeq, *run.SourceJournalSequenceMax)
	}
}

// TestIncrementalBoundaryInterleavedCompletionRefusesPartialPublish is an interleaving
// race regression test:
// 1. Accounting captures currentMax boundary N where Frame 2 has only emitted up to event N.
// 2. A hook pauses accounting right after boundary capture and commits the completion
//    event (SamplingResidual, event N+1) for the same Frame 2.
// 3. Accounting resumes boundary calculation. Invariant: evidence committed after the captured
//    boundary must NOT prove or partially publish Frame 2. The cut must step back to the
//    previous complete frame (Frame 1), refusing to partially publish up to N.
// 4. Only the subsequent accounting round (with a newly captured boundary covering N+1)
//    advances to publish the complete Frame 2.
func TestIncrementalBoundaryInterleavedCompletionRefusesPartialPublish(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()
	db, closeDB := runEquivalenceDB(t, dbPath)
	defer closeDB()

	sess := equivalenceFixtureSession
	s, err := OpenSQLiteSink(ctx, dbPath, sess, "v-race")
	if err != nil {
		t.Fatal(err)
	}

	proxyChains := []string{"NodeA", "GroupX"}

	// Frame 1: Complete frame with ConnectionNew and SamplingResidual.
	ts1 := base.Add(time.Second)
	emitEquivalenceFrame(t, s, 1, ts1, func(f, e int64) []*types.CollectorEvent {
		return []*types.CollectorEvent{
			evNew("conn-1", 100, 200, 100, 200, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
			{
				Type:      types.EventSamplingResidual,
				Timestamp: ts1,
				Details: map[string]any{
					"residualUpload":   int64(0),
					"residualDownload": int64(0),
				},
			},
		}
	})

	// Seed initial generation to publish Frame 1.
	for {
		gen, genErr := GetActiveAccountingGeneration(ctx, db)
		if genErr == nil && gen != nil {
			break
		}
		if _, err := AdvanceAccountingV2(ctx, db, "seed frame 1", 0); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	gen, err := GetActiveAccountingGeneration(ctx, db)
	if err != nil || gen == nil {
		t.Fatalf("expected active generation, got gen=%v err=%v", gen, err)
	}
	p1 := gen.PublishedJournalSequence
	if p1 <= 0 {
		t.Fatalf("expected positive published sequence, got %d", p1)
	}

	// Frame 2: Emit the first event (ConnectionDelta, sequence p1 + 1).
	ts2 := base.Add(2 * time.Second)
	evDelta := &types.CollectorEvent{
		EventID:                 "ev-f2-interleaved-delta",
		SessionID:               sess,
		EpochID:                 1,
		FrameSequence:           2,
		EventSequence:           1,
		Type:                    types.EventConnectionDelta,
		Timestamp:               ts2,
		ConnectionID:            "conn-1",
		ObservedUploadCounter:   200,
		ObservedDownloadCounter: 400,
		DeltaUpload:             100,
		DeltaDownload:           200,
		Route:                   types.RouteProxy,
		AttributionClass:        types.ClassKnownApplication,
		Metadata:                fixtureProxyMeta,
		Rule:                    "MATCH",
		Chains:                  proxyChains,
	}
	if err := s.Emit(evDelta); err != nil {
		t.Fatalf("failed to emit delta: %v", err)
	}

	// Interleaving hook: after accounting captures currentMax (= p1 + 1),
	// before it calculates the cut, emit Frame 2's SamplingResidual (sequence p1 + 2).
	hookFired := false
	onAfterCaptureBoundaryHook = func(capturedBoundary int64) {
		if capturedBoundary == p1+1 && !hookFired {
			hookFired = true
			evResidual := &types.CollectorEvent{
				EventID:       "ev-f2-interleaved-residual",
				SessionID:     sess,
				EpochID:       1,
				FrameSequence: 2,
				EventSequence: 2,
				Type:          types.EventSamplingResidual,
				Timestamp:     ts2,
				Details: map[string]any{
					"residualUpload":   int64(0),
					"residualDownload": int64(0),
				},
			}
			if err := s.Emit(evResidual); err != nil {
				t.Errorf("hook emit residual failed: %v", err)
			}
		}
	}
	defer func() { onAfterCaptureBoundaryHook = nil }()

	// Execute accounting with the active race condition hook.
	run, err := AdvanceAccountingV2(ctx, db, "race tick", 0)
	if err != nil {
		t.Fatalf("AdvanceAccountingV2 during race failed: %v", err)
	}
	if !hookFired {
		t.Fatalf("expected interleaving hook to fire during boundary capture")
	}

	// Invariant check: The published boundary must stay at p1 (Frame 1 end)!
	// It MUST NOT have published to p1 + 1 (partial Frame 2)!
	if *run.SourceJournalSequenceMax != p1 {
		t.Fatalf("RACE CONDITION VIOLATION: boundary advanced to %d into partial frame! want %d",
			*run.SourceJournalSequenceMax, p1)
	}

	// Clear hook so normal capture proceeds.
	onAfterCaptureBoundaryHook = nil

	// Next round: now that Frame 2 was fully completed and the next capture sees p1 + 2,
	// the published boundary advances cleanly to the complete Frame 2 end.
	run2, err := AdvanceAccountingV2(ctx, db, "post-race tick", 0)
	if err != nil {
		t.Fatalf("post-race AdvanceAccountingV2 failed: %v", err)
	}
	if *run2.SourceJournalSequenceMax != p1+2 {
		t.Fatalf("expected boundary to advance to complete frame end %d, got %d",
			p1+2, *run2.SourceJournalSequenceMax)
	}
}

// TestIncrementalBoundaryRefusesRecoveryFrameWithGapClosedOnly verifies that when
// a recovery frame starts with MonitoringGapClosed followed by connection events
// and SamplingResidual, committing ONLY the MonitoringGapClosed does not cause
// accounting to treat the frame as complete. Accounting must refuse to advance
// until connection evidence and SamplingResidual are committed.
func TestIncrementalBoundaryRefusesRecoveryFrameWithGapClosedOnly(t *testing.T) {
	ctx := context.Background()
	base := fixtureBase()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()
	db, closeDB := runEquivalenceDB(t, dbPath)
	defer closeDB()

	sess := equivalenceFixtureSession
	s, err := OpenSQLiteSink(ctx, dbPath, sess, "v-boundary-recovery")
	if err != nil {
		t.Fatal(err)
	}

	proxyChains := []string{"NodeA", "GroupX"}
	// Frame 1: Complete initial frame that includes a gap open and ends with SamplingResidual.
	ts1 := base.Add(time.Second)
	emitEquivalenceFrame(t, s, 1, ts1, func(f, e int64) []*types.CollectorEvent {
		return []*types.CollectorEvent{
			evNew("conn-1", 100, 200, 100, 200, types.RouteProxy, types.ClassKnownApplication, fixtureProxyMeta, "MATCH", proxyChains),
			{
				Type:                types.EventMonitoringGapOpened,
				Timestamp:           ts1,
				AttributionInterval: []string{ts1.Format(time.RFC3339Nano)},
				Details: map[string]any{
					"source": "controller_stream",
					"reason": "mock_stream_disconnect",
				},
			},
			{
				Type:      types.EventSamplingResidual,
				Timestamp: ts1,
				Details: map[string]any{
					"residualUpload":   int64(0),
					"residualDownload": int64(0),
				},
			},
		}
	})

	// Seed initial generation to publish frame 1.
	for {
		gen, genErr := GetActiveAccountingGeneration(ctx, db)
		if genErr == nil && gen != nil {
			break
		}
		if _, err := AdvanceAccountingV2(ctx, db, "seed frame 1", 0); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	gen, err := GetActiveAccountingGeneration(ctx, db)
	if err != nil || gen == nil {
		t.Fatalf("expected active generation, got gen=%v err=%v", gen, err)
	}
	p1 := gen.PublishedJournalSequence
	if p1 <= 0 {
		t.Fatalf("expected positive published sequence, got %d", p1)
	}

	// Frame 2: Recovery frame in progress.
	// Emits MonitoringGapClosed first.
	ts2 := base.Add(2 * time.Second)
	evGapClosed := &types.CollectorEvent{
		EventID:             "ev-f2-gap-closed",
		SessionID:           sess,
		EpochID:             1,
		FrameSequence:       2,
		EventSequence:       1,
		Type:                types.EventMonitoringGapClosed,
		Timestamp:           ts2,
		AttributionInterval: []string{ts1.Format(time.RFC3339Nano), ts2.Format(time.RFC3339Nano)},
		Details: map[string]any{
			"gapPhysicalDeltaUnavailable": false,
		},
	}
	if err := s.Emit(evGapClosed); err != nil {
		t.Fatalf("failed to emit gap closed: %v", err)
	}

	// Attempt accounting advance when only GapClosed is present.
	run1, err := AdvanceAccountingV2(ctx, db, "tick with GapClosed only", 0)
	if err != nil {
		t.Fatalf("AdvanceAccountingV2 failed: %v", err)
	}
	// Must NOT advance past p1 because Frame 2 is still incomplete.
	if run1 != nil && run1.SourceJournalSequenceMax != nil && *run1.SourceJournalSequenceMax > p1 {
		t.Fatalf("PARTIAL PUBLISH: advanced boundary to %d when recovery frame only had MonitoringGapClosed; want %d",
			*run1.SourceJournalSequenceMax, p1)
	}

	// Verify generation published sequence is still p1.
	genAfter1, err := GetActiveAccountingGeneration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if genAfter1.PublishedJournalSequence != p1 {
		t.Fatalf("expected generation published sequence to remain %d, got %d",
			p1, genAfter1.PublishedJournalSequence)
	}

	// Now emit the remainder of Frame 2: ConnectionDelta and SamplingResidual.
	evDelta := &types.CollectorEvent{
		EventID:                 "ev-f2-delta",
		SessionID:               sess,
		EpochID:                 1,
		FrameSequence:           2,
		EventSequence:           2,
		Type:                    types.EventConnectionDelta,
		Timestamp:               ts2,
		ConnectionID:            "conn-1",
		ObservedUploadCounter:   300,
		ObservedDownloadCounter: 600,
		DeltaUpload:             200,
		DeltaDownload:           400,
		Route:                   types.RouteProxy,
		AttributionClass:        types.ClassKnownApplication,
		Metadata:                fixtureProxyMeta,
		Rule:                    "MATCH",
		Chains:                  proxyChains,
	}
	if err := s.Emit(evDelta); err != nil {
		t.Fatalf("failed to emit delta: %v", err)
	}

	evResidual := &types.CollectorEvent{
		EventID:       "ev-f2-residual",
		SessionID:     sess,
		EpochID:       1,
		FrameSequence: 2,
		EventSequence: 3,
		Type:          types.EventSamplingResidual,
		Timestamp:     ts2,
		Details: map[string]any{
			"residualUpload":   int64(0),
			"residualDownload": int64(0),
		},
	}
	if err := s.Emit(evResidual); err != nil {
		t.Fatalf("failed to emit residual: %v", err)
	}

	// Advance accounting again: should cleanly advance across the completed Frame 2.
	run2, err := AdvanceAccountingV2(ctx, db, "tick with complete recovery frame", 0)
	if err != nil {
		t.Fatalf("AdvanceAccountingV2 after completion failed: %v", err)
	}
	expectedEnd := p1 + 3 // gapClosed + delta + residual = 3 events
	if run2 == nil || run2.SourceJournalSequenceMax == nil || *run2.SourceJournalSequenceMax != expectedEnd {
		var got int64
		if run2 != nil && run2.SourceJournalSequenceMax != nil {
			got = *run2.SourceJournalSequenceMax
		}
		t.Fatalf("expected boundary to advance to complete frame end %d, got %d", expectedEnd, got)
	}

	genAfter2, err := GetActiveAccountingGeneration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if genAfter2.PublishedJournalSequence != expectedEnd {
		t.Fatalf("expected generation published sequence to be %d, got %d",
			expectedEnd, genAfter2.PublishedJournalSequence)
	}
}
