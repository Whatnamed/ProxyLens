package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"testing"
	"time"

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

// TestIncrementalConstantCost is the architectural guard proving incremental
// cost depends on the appended range, not on history size: the same appended
// batch on a large-history generation must not be orders of magnitude slower
// than on a small-history generation. (The full 1.5M-row proof runs in the
// E-drive scale acceptance tool.)
func TestIncrementalConstantCost(t *testing.T) {
	if testing.Short() {
		t.Skip("constant-cost test skipped in short mode")
	}
	ctx := context.Background()
	base := fixtureBase()

	buildAndMeasure := func(t *testing.T, historyEvents int) time.Duration {
		dbPath, cleanup := createAccountingTestDB(t)
		defer cleanup()
		db, closeDB := runEquivalenceDB(t, dbPath)
		defer closeDB()

		s, err := OpenSQLiteSink(ctx, dbPath, equivalenceFixtureSession, "v-eq")
		if err != nil {
			t.Fatal(err)
		}
		emitEquivalenceFrameScript(t, s, 1, base.Add(time.Second))
		if err := bulkInsertJournalRows(t, db, historyEvents); err != nil {
			t.Fatal(err)
		}

		// Seed to activation (may span multiple chunked calls).
		for {
			gen, genErr := GetActiveAccountingGeneration(ctx, db)
			if genErr == nil && gen != nil {
				break
			}
			if _, err := AdvanceAccountingV2(ctx, db, "seed", 0); err != nil {
				t.Fatal(err)
			}
		}

		// Identical appended batch for both histories, inserted directly into
		// the journal beyond the published boundary.
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

		start := time.Now()
		if _, err := AdvanceAccountingV2(ctx, db, "appended batch", 0); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)

		fresh, err := NewAnalyticsService(db).GetAccountingFreshness(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !fresh.IsFresh {
			t.Fatalf("history %d: expected fresh after incremental, got %+v", historyEvents, fresh)
		}
		return elapsed
	}

	small := buildAndMeasure(t, 10000)
	large := buildAndMeasure(t, 300000)
	ratio := float64(large) / float64(small)
	t.Logf("incremental duration: small-history(10k)=%v large-history(300k)=%v ratio=%.1fx", small, large, ratio)
	if ratio >= 20 {
		t.Fatalf("large history made a small incremental batch %.0fx slower (small=%v large=%v)", ratio, small, large)
	}
}
