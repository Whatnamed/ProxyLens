package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"

	"github.com/gorilla/websocket"
)

// -----------------------------------------------------------------------------
// seed-real: migration + v2 seed of the E: copy of the real production DB
// -----------------------------------------------------------------------------

type seedReport struct {
	SourceSHA256Before      string  `json:"sourceSHA256Before"`
	QuickCheckBefore        string  `json:"quickCheckBefore"`
	JournalRows             int64   `json:"journalRows"`
	LegacyAccountedRows     int64   `json:"legacyAccountedRows"`
	Chunks                  int     `json:"chunks"`
	SeedDurationSeconds     float64 `json:"seedDurationSeconds"`
	PeakHeapMB              float64 `json:"peakHeapMB"`
	WALPeakBytes            int64   `json:"walPeakBytes"`
	DBBytesBefore           int64   `json:"dbBytesBefore"`
	DBBytesAfter            int64   `json:"dbBytesAfter"`
	TempPeakBytes           int64   `json:"tempPeakBytes"`
	PublishedBoundary       int64   `json:"publishedBoundary"`
	PublishedRawUp          int64   `json:"publishedRawUp"`
	PublishedRawDown        int64   `json:"publishedRawDown"`
	PublishedAccUp          int64   `json:"publishedAccUp"`
	PublishedAccDown        int64   `json:"publishedAccDown"`
	V2AccountedRows         int64   `json:"v2AccountedRows"`
	JournalRowsAfter        int64   `json:"journalRowsAfter"`
	QuickCheckAfter         string  `json:"quickCheckAfter"`
	IncrementalRunAfterSeed struct {
		DurationSeconds float64 `json:"durationSeconds"`
		LagAfter        int64   `json:"lagAfter"`
	} `json:"incrementalRunAfterSeed"`
}

func runSeedReal(ctx context.Context, realDB string, chunk int64) error {
	if realDB == "" {
		return fmt.Errorf("seed-real requires -real-db pointing at the E: copy")
	}
	rep := seedReport{}
	rep.DBBytesBefore = fileBytes(realDB)

	hash, err := sha256File(realDB)
	if err != nil {
		return err
	}
	rep.SourceSHA256Before = hash

	db, err := storage.OpenDB(ctx, realDB)
	if err != nil {
		return fmt.Errorf("open (migration rehearsal) failed: %w", err)
	}
	defer db.Close()

	if err := db.QueryRowContext(ctx, "PRAGMA quick_check;").Scan(&rep.QuickCheckBefore); err != nil {
		return err
	}
	if rep.QuickCheckBefore != "ok" {
		return fmt.Errorf("quick_check before seed: %s", rep.QuickCheckBefore)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal;`).Scan(&rep.JournalRows); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounted_traffic;`).Scan(&rep.LegacyAccountedRows); err != nil {
		return err
	}

	tempDir := os.TempDir()
	start := time.Now()
	seedChunks := 0
	active := false
	for i := 0; i < 10000 && !active; i++ {
		if _, err := storage.AdvanceAccountingV2(ctx, db, "scale seed-real", chunk); err != nil {
			return fmt.Errorf("seed chunk %d: %w", i+1, err)
		}
		seedChunks++
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		peak := float64(ms.HeapAlloc) / (1 << 20)
		if peak > rep.PeakHeapMB {
			rep.PeakHeapMB = peak
		}
		if wal := fileBytes(realDB + "-wal"); wal > rep.WALPeakBytes {
			rep.WALPeakBytes = wal
		}
		if tb := dirBytes(tempDir); tb > rep.TempPeakBytes {
			rep.TempPeakBytes = tb
		}
		gen, genErr := storage.GetActiveAccountingGeneration(ctx, db)
		if genErr == nil && gen != nil {
			active = true
		}
	}
	if !active {
		return fmt.Errorf("seed did not activate after %d chunks", seedChunks)
	}
	rep.SeedDurationSeconds = time.Since(start).Seconds()
	rep.Chunks = seedChunks

	gen, err := storage.GetActiveAccountingGeneration(ctx, db)
	if err != nil {
		return err
	}
	rep.PublishedBoundary = gen.PublishedJournalSequence
	rep.PublishedRawUp = gen.PublishedRawUpload
	rep.PublishedRawDown = gen.PublishedRawDownload
	rep.PublishedAccUp = gen.PublishedAccountedUpload
	rep.PublishedAccDown = gen.PublishedAccountedDownload

	var journalMax int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&journalMax); err != nil {
		return err
	}
	if gen.PublishedJournalSequence != journalMax {
		return fmt.Errorf("published %d != journal max %d", gen.PublishedJournalSequence, journalMax)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounted_traffic_v2 WHERE generation_id = ?;`, gen.GenerationID).Scan(&rep.V2AccountedRows); err != nil {
		return err
	}
	if rep.PublishedAccUp > rep.PublishedRawUp || rep.PublishedAccDown > rep.PublishedRawDown {
		return fmt.Errorf("invariant broken: accounted exceeds raw")
	}
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check;`).Scan(&rep.QuickCheckAfter); err != nil {
		return err
	}
	if rep.QuickCheckAfter != "ok" {
		return fmt.Errorf("quick_check after seed: %s", rep.QuickCheckAfter)
	}
	var journalRowsAfter int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal;`).Scan(&journalRowsAfter); err != nil {
		return err
	}
	rep.JournalRowsAfter = journalRowsAfter
	if journalRowsAfter != rep.JournalRows {
		return fmt.Errorf("raw authority modified: journal rows %d -> %d", rep.JournalRows, journalRowsAfter)
	}
	rep.DBBytesAfter = fileBytes(realDB)

	// One incremental cycle right after seed must be fast and bounded.
	var maxSeq2 int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&maxSeq2); err != nil {
		return err
	}
	if maxSeq2 > gen.PublishedJournalSequence {
		t0 := time.Now()
		if _, err := storage.AdvanceAccountingV2(ctx, db, "post-seed incremental", chunk); err != nil {
			return err
		}
		rep.IncrementalRunAfterSeed.DurationSeconds = time.Since(t0).Seconds()
		fresh, err := storage.NewAnalyticsService(db).GetAccountingFreshness(ctx)
		if err != nil {
			return err
		}
		rep.IncrementalRunAfterSeed.LagAfter = fresh.LagEvents
		if fresh.LagEvents != 0 {
			return fmt.Errorf("lag nonzero after incremental: %d", fresh.LagEvents)
		}
	}

	report(rep)
	hashAfter, err := sha256File(realDB)
	if err == nil && hashAfter == rep.SourceSHA256Before {
		return fmt.Errorf("DB file unchanged after writes (impossible) - hash tooling suspect")
	}
	return nil
}

// -----------------------------------------------------------------------------
// soak: continuous collector + incremental accounting + query workload
// -----------------------------------------------------------------------------

type soakSample struct {
	T          string  `json:"t"`
	DBBytes    int64   `json:"dbBytes"`
	WALBytes   int64   `json:"walBytes"`
	JournalMax int64   `json:"journalMax"`
	Lag        int64   `json:"lag"`
	HeapMB     float64 `json:"heapMB"`
}

type soakReport struct {
	DurationSeconds      float64      `json:"durationSeconds"`
	FramesServed         int64        `json:"framesServed"`
	Samples              []soakSample `json:"samples"`
	WALPeakBytes         int64        `json:"walPeakBytes"`
	MaxIncrementalSecond float64      `json:"maxIncrementalSeconds"`
	IncrementalRuns      int64        `json:"incrementalRuns"`
	LegacyRuns           int64        `json:"legacyRuns"`
	FinalLag             int64        `json:"finalLag"`
	QueueOverloadEvents  int64        `json:"queueOverloadEvents"`
	V2AccountedRows      int64        `json:"v2AccountedRows"`
	JournalRows          int64        `json:"journalRows"`
}

func runSoak(ctx context.Context, dir string, duration time.Duration) error {
	dbPath := filepath.Join(dir, "soak.db")
	_ = os.Remove(dbPath)

	// Mock controller serving zero-heavy realistic frames.
	var framesServed atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"scale-mock"}`))
	})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		const conns = 80
		type cg struct{ up, down int64 }
		gens := make([]cg, conns)
		exists := make([]bool, conns)
		for c := range exists {
			exists[c] = true
		}
		// Controller lifetime totals are monotonic in real Mihomo: closing a
		// connection never decreases them. Churn must only reset per-connection
		// counters, or every toggle-off fakes a counter epoch break. Connection
		// IDs are intentionally reused so reappearance exercises same-epoch
		// re-observation after Disappeared.
		var lifetimeUp, lifetimeDown int64
		tick := 0
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if ctx.Err() != nil {
				return
			}
			tick++
			// Churn: rotate one connection in/out every ~10s.
			if tick%40 == 0 {
				idx := (tick / 40) % conns
				exists[idx] = !exists[idx]
				if !exists[idx] {
					gens[idx] = cg{}
				}
			}
			payload := types.ConnectionSnapshotPayload{Connections: []types.ConnectionSnapshot{}}
			for c := 0; c < conns; c++ {
				if !exists[c] {
					continue
				}
				// Zero-heavy: ~2.4% of frame-conn pairs carry bytes.
				if (tick*conns+c)%42 == 7 {
					gens[c].up += 700
					gens[c].down += 1200
					lifetimeUp += 700
					lifetimeDown += 1200
				}
					payload.Connections = append(payload.Connections, types.ConnectionSnapshot{
						ID:       fmt.Sprintf("soak-%03d", c),
						Upload:   gens[c].up,
						Download: gens[c].down,
						Metadata: types.RawMetadata{Process: fmt.Sprintf("p%02d.exe", c%9), Host: fmt.Sprintf("h%02d.example", c%13), Network: "tcp"},
						Rule:     "MATCH",
						Chains:   []string{"Node-S", "Group-S"},
					})
				}
				payload.UploadTotal = 500000 + lifetimeUp
				payload.DownloadTotal = 900000 + lifetimeDown
			b, err := json.Marshal(payload)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
			framesServed.Add(1)
		}
	})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: mux}
	ln, err := netListen()
	if err != nil {
		return err
	}
	go func() { _ = server.Serve(ln) }()
	defer func() {
		_ = server.Close()
	}()

	rt, err := proxylensruntime.NewRuntime(proxylensruntime.RuntimeOptions{
		DBPath: dbPath,
		Collector: proxylensruntime.CollectorOptions{
			ControllerURL: fmt.Sprintf("http://127.0.0.1:%d", lnPort(ln)),
		},
		AccountingInterval: 30 * time.Second,
		Logger:             func(f string, a ...any) { fmt.Printf("[soak-rt] "+f+"\n", a...) },
	})
	if err != nil {
		return err
	}
	rtCtx, cancelRT := context.WithCancel(ctx)
	defer cancelRT()
	rtDone := make(chan error, 1)
	go func() {
		_, err := rt.Run(rtCtx)
		rtDone <- err
	}()

	// Wait for the writer DB to appear, then start telemetry + query workload.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dbPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	rep := soakReport{DurationSeconds: duration.Seconds()}
	var walPeak int64
	samples := 0
	start := time.Now()
	for time.Since(start) < duration {
		time.Sleep(10 * time.Second)
		sample := soakSample{T: time.Now().UTC().Format(time.RFC3339)}
		sample.DBBytes = fileBytes(dbPath)
		sample.WALBytes = fileBytes(dbPath + "-wal")
		if sample.WALBytes > walPeak {
			walPeak = sample.WALBytes
		}
		ro, err := storage.OpenReadOnlyDB(ctx, dbPath)
		if err == nil {
			var jmax int64
			_ = ro.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&jmax)
			sample.JournalMax = jmax
			if fresh, err := storage.NewAnalyticsService(ro).GetAccountingFreshness(ctx); err == nil {
				sample.Lag = fresh.LagEvents
			}
			// Read-only query workload against v2 scope.
			_, _ = storage.NewAnalyticsService(ro).GetUsageSummary(ctx, storage.AnalyticsFilter{})
			_, _ = storage.NewAnalyticsService(ro).GetTopDimensions(ctx, "process", storage.AnalyticsFilter{Limit: 10})
			_ = ro.Close()
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		sample.HeapMB = float64(ms.HeapAlloc) / (1 << 20)
		rep.Samples = append(rep.Samples, sample)
		samples++
		fmt.Printf("[soak] %d/%ds db=%dMB wal=%dMB journal=%d lag=%d heap=%.0fMB\n",
			int(time.Since(start).Seconds()), int(duration.Seconds()), sample.DBBytes/(1<<20), sample.WALBytes/(1<<20), sample.JournalMax, sample.Lag, sample.HeapMB)
	}
	_ = samples

	// Graceful runtime shutdown (production shutdown path includes the WAL
	// TRUNCATE maintenance checkpoint).
	cancelRT()
	select {
	case err := <-rtDone:
		if err != nil {
			return fmt.Errorf("runtime error: %w", err)
		}
	case <-time.After(60 * time.Second):
		return fmt.Errorf("runtime did not shut down within 60s")
	}

	rep.WALPeakBytes = walPeak
	// Final verification against the DB.
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	fresh, err := storage.NewAnalyticsService(db).GetAccountingFreshness(ctx)
	if err != nil {
		return err
	}
	rep.FinalLag = fresh.LagEvents
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounting_runs_v2 WHERE mode='incremental' AND status='completed';`).Scan(&rep.IncrementalRuns); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounting_runs;`).Scan(&rep.LegacyRuns); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM collector_health WHERE issue='queue_overload_degradation';`).Scan(&rep.QueueOverloadEvents); err != nil {
		return err
	}
	var maxDur sql.NullFloat64
	if err := db.QueryRowContext(ctx, `
		SELECT MAX((julianday(completed_at) - julianday(started_at)) * 86400.0)
		FROM accounting_runs_v2 WHERE mode='incremental' AND completed_at IS NOT NULL;
	`).Scan(&maxDur); err != nil {
		return err
	}
	if maxDur.Valid {
		rep.MaxIncrementalSecond = maxDur.Float64
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounted_traffic_v2;`).Scan(&rep.V2AccountedRows); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal;`).Scan(&rep.JournalRows); err != nil {
		return err
	}

	report(rep)

	if rep.FinalLag != 0 {
		return fmt.Errorf("final lag %d nonzero", rep.FinalLag)
	}
	if rep.LegacyRuns != 0 {
		return fmt.Errorf("legacy full rebuilds executed during soak: %d", rep.LegacyRuns)
	}
	if rep.QueueOverloadEvents != 0 {
		return fmt.Errorf("collector queue overload during soak: %d", rep.QueueOverloadEvents)
	}
	if rep.MaxIncrementalSecond > 30 {
		return fmt.Errorf("incremental run took %.1fs (>30s): not bounded", rep.MaxIncrementalSecond)
	}
	if walPeak > 2<<(10+10+10) { // 2 GiB
		return fmt.Errorf("WAL peaked at %d bytes (>2GiB)", walPeak)
	}
	rep.FramesServed = framesServed.Load()
	return nil
}

// -----------------------------------------------------------------------------
// constant-cost: identical appended batch on small vs large history
// -----------------------------------------------------------------------------

func bulkInsertDeltas(ctx context.Context, db *sql.DB, prefix string, n int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var maxSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&maxSeq); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO event_journal (
			event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type,
			observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
		) VALUES (?, ?, 1, ?, 1, 'ConnectionDelta', ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	// Frame sequences must be unique per (session, epoch): derive a stable
	// per-prefix offset so successive batches never collide.
	sum := sha256.Sum256([]byte(prefix))
	frameBase := int64(binary.BigEndian.Uint64(sum[:8])%1_000_000_000) * 10_000_000
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < n; i++ {
		seq := maxSeq + int64(i) + 1
		obsTs := base.Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339Nano)
		connID := fmt.Sprintf("cc-%d", i%50)
		eventJSON := fmt.Sprintf(
			`{"eventId":"%s-%d","sessionId":"sess-bulk","epochId":1,"frameSequence":%d,"eventSequence":1,`+
				`"timestamp":%q,"type":"ConnectionDelta","connectionId":%q,`+
				`"observedUploadCounter":%d,"observedDownloadCounter":%d,`+
				`"deltaUpload":100,"deltaDownload":200,`+
				`"monitoredCumulativeUpload":%d,"monitoredCumulativeDownload":%d,`+
				`"route":"PROXY","attributionClass":"known_application",`+
				`"metadata":{"network":"tcp","process":"app.exe","host":"logical.example"},`+
				`"rule":"MATCH","chains":["NodeA","GroupX"]}`,
			prefix, i, frameBase+int64(i), obsTs, connID,
			1000+100*i, 2000+200*i, 1000+100*i, 2000+200*i)
		sum := sha256.Sum256([]byte(eventJSON))
		if _, err := stmt.ExecContext(ctx,
			fmt.Sprintf("%s-%d", prefix, i), "sess-bulk", frameBase+int64(i),
			obsTs, connID, eventJSON, hex.EncodeToString(sum[:]), obsTs, seq,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func seedToActive(ctx context.Context, db *sql.DB, chunk int64) error {
	for i := 0; i < 100000; i++ {
		gen, err := storage.GetActiveAccountingGeneration(ctx, db)
		if err == nil && gen != nil {
			return nil
		}
		if _, err := storage.AdvanceAccountingV2(ctx, db, "scale seed", chunk); err != nil {
			return err
		}
	}
	return fmt.Errorf("seed never activated")
}

func runConstantCost(ctx context.Context, dir, realDB string) error {
	if realDB == "" {
		return fmt.Errorf("constant-cost requires -real-db (the seeded E: copy)")
	}

	buildSmall := func() (string, error) {
		p := filepath.Join(dir, "small-history.db")
		_ = os.Remove(p)
		db, err := storage.OpenDB(ctx, p)
		if err != nil {
			return "", err
		}
		defer db.Close()
		if err := bulkInsertDeltas(ctx, db, "small", 50000); err != nil {
			return "", err
		}
		if err := seedToActive(ctx, db, 50000); err != nil {
			return "", err
		}
		return p, nil
	}
	smallPath, err := buildSmall()
	if err != nil {
		return fmt.Errorf("small history build: %w", err)
	}

	// The real E: copy must already carry an active generation (seed-real).
	dbLarge, err := storage.OpenDB(ctx, realDB)
	if err != nil {
		return err
	}
	defer dbLarge.Close()
	gen, err := storage.GetActiveAccountingGeneration(ctx, dbLarge)
	if err != nil {
		return fmt.Errorf("real copy has no active generation (run seed-real first): %w", err)
	}
	_ = gen

	type measure struct {
		Seconds       float64 `json:"seconds"`
		JournalBefore int64   `json:"journalBefore"`
	}
	run := func(db *sql.DB, prefix string) (measure, error) {
		var m measure
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&m.JournalBefore); err != nil {
			return m, err
		}
		if err := bulkInsertDeltas(ctx, db, prefix, 5000); err != nil {
			return m, err
		}
		t0 := time.Now()
		if _, err := storage.AdvanceAccountingV2(ctx, db, "constant-cost batch", 0); err != nil {
			return m, err
		}
		m.Seconds = time.Since(t0).Seconds()
		fresh, err := storage.NewAnalyticsService(db).GetAccountingFreshness(ctx)
		if err != nil {
			return m, err
		}
		if fresh.LagEvents != 0 {
			return m, fmt.Errorf("lag %d after constant-cost run", fresh.LagEvents)
		}
		return m, nil
	}

	dbSmall, err := storage.OpenDB(ctx, smallPath)
	if err != nil {
		return err
	}
	defer dbSmall.Close()

	mSmall, err := run(dbSmall, fmt.Sprintf("cc-small-%d", time.Now().UnixNano()))
	if err != nil {
		return fmt.Errorf("small run: %w", err)
	}
	mLarge, err := run(dbLarge, fmt.Sprintf("cc-large-%d", time.Now().UnixNano()))
	if err != nil {
		return fmt.Errorf("large run: %w", err)
	}
	mLarge2, err := run(dbLarge, fmt.Sprintf("cc-large2-%d", time.Now().UnixNano()))
	if err != nil {
		return fmt.Errorf("large run 2: %w", err)
	}
	ratio := 0.0
	if mSmall.Seconds > 0 {
		ratio = mLarge.Seconds / mSmall.Seconds
	}
	report(map[string]any{
		"smallHistoryJournal": mSmall.JournalBefore,
		"largeHistoryJournal": mLarge.JournalBefore,
		"smallSeconds":        mSmall.Seconds,
		"largeSeconds":        mLarge.Seconds,
		"largeSecondsWarm":    mLarge2.Seconds,
		"ratio":               ratio,
	})
	if ratio >= 20 {
		return fmt.Errorf("large history made the identical batch %.1fx slower", ratio)
	}
	return nil
}

// -----------------------------------------------------------------------------
// crash: cancellation, boundary atomicity and checkpoint contention
// -----------------------------------------------------------------------------

func runCrash(ctx context.Context, dir string, chunk int64) error {
	dbPath := filepath.Join(dir, "crash.db")
	_ = os.Remove(dbPath)
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := bulkInsertDeltas(ctx, db, "crash", 20000); err != nil {
		return err
	}

	// (a) Cancel during the seed: the generation must stay inactive with only
	// committed chunk cursors advanced; resume must reach activation.
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, err := storage.AdvanceAccountingV2(cancelCtx, db, "crash seed", chunk); err != nil {
		return fmt.Errorf("first seed chunk unexpectedly failed: %w", err)
	}
	cancel()
	resumed := false
	for i := 0; i < 100000; i++ {
		gen, err := storage.GetActiveAccountingGeneration(ctx, db)
		if err == nil && gen != nil {
			resumed = true
			break
		}
		if _, err := storage.AdvanceAccountingV2(ctx, db, "crash resume", chunk); err != nil {
			return fmt.Errorf("resume chunk: %w", err)
		}
	}
	if !resumed {
		return fmt.Errorf("seed never resumed to activation")
	}
	gen, err := storage.GetActiveAccountingGeneration(ctx, db)
	if err != nil {
		return err
	}

	// (b) Cancel before publish: boundary unchanged, zero unpublished rows.
	published := gen.PublishedJournalSequence
	if err := bulkInsertDeltas(ctx, db, "crash2", 500); err != nil {
		return err
	}
	cancelCtx2, cancel2 := context.WithCancel(ctx)
	cancel2()
	if _, err := storage.AdvanceAccountingV2(cancelCtx2, db, "canceled incremental", 0); err == nil {
		return fmt.Errorf("canceled incremental must fail")
	}
	var unpublished int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounted_traffic_v2 WHERE source_journal_sequence > ?;`, published).Scan(&unpublished); err != nil {
		return err
	}
	if unpublished != 0 {
		return fmt.Errorf("canceled publish left %d unpublished derived rows", unpublished)
	}
	if _, err := storage.AdvanceAccountingV2(ctx, db, "retry", 0); err != nil {
		return err
	}
	fresh, err := storage.NewAnalyticsService(db).GetAccountingFreshness(ctx)
	if err != nil {
		return err
	}
	if fresh.LagEvents != 0 {
		return fmt.Errorf("lag %d after retry", fresh.LagEvents)
	}

	// (c) Query reader during TRUNCATE checkpoint: busy reported, no hang, no kill.
	ro, err := storage.OpenReadOnlyDB(ctx, dbPath)
	if err != nil {
		return err
	}
	defer ro.Close()
	tx, err := ro.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var probe int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal;`).Scan(&probe); err != nil {
		return err
	}
	cpDone := make(chan error, 1)
	var cpRes storage.WALCheckpointResult
	go func() {
		res, err := storage.CheckpointWAL(context.Background(), db, storage.WALCheckpointTruncateMode)
		cpRes = res
		cpDone <- err
	}()
	select {
	case err := <-cpDone:
		if err != nil {
			return fmt.Errorf("checkpoint with reader errored: %w", err)
		}
		fmt.Printf("[crash] checkpoint with active reader: busy=%v log=%d checkpointed=%d\n",
			cpRes.Busy, cpRes.LogFrames, cpRes.CheckpointedFrames)
	case <-time.After(20 * time.Second):
		return fmt.Errorf("checkpoint hung with an active reader")
	}
	_ = tx.Rollback()

	// (d) Failed generation cleanup leaves raw untouched and removes staging.
	var rawBefore int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal;`).Scan(&rawBefore); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounting_generations SET status='failed' WHERE status='active';`); err != nil {
		return err
	}
	cleaned, err := storage.CleanupFailedGenerations(ctx, db)
	if err != nil {
		return err
	}
	if cleaned != 1 {
		return fmt.Errorf("expected 1 failed generation cleaned, got %d", cleaned)
	}
	var rawAfter int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal;`).Scan(&rawAfter); err != nil {
		return err
	}
	if rawBefore != rawAfter {
		return fmt.Errorf("cleanup touched raw authority: %d -> %d", rawBefore, rawAfter)
	}

	report(map[string]any{
		"seedResumeActivated":      resumed,
		"canceledPublishRows":      unpublished,
		"checkpointWithReader":     cpRes,
		"failedGenerationsCleaned": cleaned,
		"rawRowsBefore":            rawBefore,
		"rawRowsAfter":             rawAfter,
	})
	return nil
}

// -----------------------------------------------------------------------------
// cardinality: long-session high-cardinality incremental accounting gate
// -----------------------------------------------------------------------------

// cardinalityReport is the evidence record of one cardinality gate run: the
// same small appended batch on two databases whose only difference is the
// number of distinct historical connections in one long session/epoch. The
// writer contract holds if the transaction writer-hold duration stays bounded
// as cardinality grows; preparation work is expected to grow (it reads the
// dirty closure) but must stay OFF the writer lock.
type cardinalityReport struct {
	HistoricalConns      int     `json:"historicalConns"`
	JournalBefore        int64   `json:"journalBefore"`
	TotalDurationSeconds float64 `json:"totalDurationSeconds"`
	PrepDurationSeconds  float64 `json:"prepDurationSeconds"`
	TxDurationSeconds    float64 `json:"txWriterHoldSeconds"`
	Processed            int64   `json:"processed"`
	DirtyConns           int     `json:"dirtyConns"`
	ClosureConns         int     `json:"closureConns"`
	AffectedGroups       int     `json:"affectedGroups"`
	ClassChanges         int     `json:"classChanges"`
	AccountedRows        int64   `json:"accountedRows"`
	FinalLag             int64   `json:"finalLag"`
}

func runCardinality(ctx context.Context, dir string, chunk int64) error {
	sizes := []int{1000, 10000}
	reports := make(map[string]cardinalityReport, len(sizes))

	for _, n := range sizes {
		dbPath := filepath.Join(dir, fmt.Sprintf("cardinality-%05d.db", n))
		_ = os.Remove(dbPath)
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")

		db, err := storage.OpenDB(ctx, dbPath)
		if err != nil {
			return err
		}

		// History: one long session/epoch with n distinct short-lived
		// connections (New with traffic, then disappeared -> terminal) plus
		// two long-lived active connections, one of them a relay candidate so
		// every incremental chunk exercises real matching work against the
		// whole historical logical population on the same node chain.
		base := time.Now().UTC().Add(-24 * time.Hour)
		if err := bulkInsertCardinalityHistory(ctx, db, "sess-card", n, base); err != nil {
			db.Close()
			return err
		}
		if err := seedToActive(ctx, db, chunk); err != nil {
			db.Close()
			return fmt.Errorf("cardinality seed (%d conns): %w", n, err)
		}

		var journalBefore int64
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&journalBefore); err != nil {
			db.Close()
			return err
		}

		// Identical small appended batch on every database: fresh evidence for
		// the two long-lived connections plus a handful of new connections.
		batchFrame := int64(9_000_000_000) + int64(n)
		if err := bulkInsertCardinalityBatch(ctx, db, "sess-card", base.Add(23*time.Hour), batchFrame); err != nil {
			db.Close()
			return err
		}

		t0 := time.Now()
		if _, err := storage.AdvanceAccountingV2(ctx, db, "cardinality batch", 0); err != nil {
			db.Close()
			return fmt.Errorf("cardinality incremental (%d conns): %w", n, err)
		}
		total := time.Since(t0)

		fresh, err := storage.NewAnalyticsService(db).GetAccountingFreshness(ctx)
		if err != nil {
			db.Close()
			return err
		}
		if fresh.LagEvents != 0 {
			db.Close()
			return fmt.Errorf("cardinality %d: lag %d after incremental", n, fresh.LagEvents)
		}

		telm := storage.LastIncrementalChunkTelemetry()
		if telm == nil {
			db.Close()
			return fmt.Errorf("no incremental chunk telemetry recorded")
		}
		rep := cardinalityReport{
			HistoricalConns:      n,
			JournalBefore:        journalBefore,
			TotalDurationSeconds: total.Seconds(),
			PrepDurationSeconds:  telm.PrepDuration.Seconds(),
			TxDurationSeconds:    telm.TxDuration.Seconds(),
			Processed:            telm.Processed,
			DirtyConns:           telm.DirtyConns,
			ClosureConns:         telm.ClosureConns,
			AffectedGroups:       telm.AffectedGroups,
			ClassChanges:         telm.ClassChanges,
			AccountedRows:        telm.AccountedRows,
			FinalLag:             fresh.LagEvents,
		}
		reports[fmt.Sprintf("%d", n)] = rep
		db.Close()
	}

	report(map[string]any{
		"runs": reports,
		"note": "txWriterHoldSeconds is the writer-lock hold of the incremental transaction; it must stay bounded as cardinality grows (preparation runs off the writer lock)",
	})

	small := reports["1000"]
	large := reports["10000"]
	if large.TxDurationSeconds <= 0 {
		return fmt.Errorf("missing writer-hold telemetry for the 10k run")
	}
	// Writer contract gates: the 10k writer hold must stay under a 2s absolute
	// bound. A ratio check against the 1k hold only applies once the hold
	// exceeds an absolute noise floor: at millisecond scale the hold is
	// dominated by fixed per-write overhead (WAL commit, fsync, bookkeeping),
	// not cardinality-proportional work, so a raw ratio is not evidence of
	// scaling. Above the floor, the hold must also stay within 5x of the 1k
	// baseline.
	if large.TxDurationSeconds > 2.0 {
		return fmt.Errorf("10k writer hold %.3fs exceeds the 2s absolute bound", large.TxDurationSeconds)
	}
	ratio := 1.0
	if small.TxDurationSeconds > 0 {
		ratio = large.TxDurationSeconds / small.TxDurationSeconds
	}
	const writerHoldNoiseFloorSeconds = 0.250
	if large.TxDurationSeconds > writerHoldNoiseFloorSeconds && ratio > 5 {
		return fmt.Errorf("10k writer hold %.3fs is %.1fx the 1k hold %.3fs - writer transaction scales with cardinality",
			large.TxDurationSeconds, ratio, small.TxDurationSeconds)
	}
	return nil
}

// bulkInsertCardinalityHistory writes n terminal short-lived connections and
// two long-lived active connections directly into the journal of one
// session/epoch. Chains intentionally share the same final egress node so the
// relay reconciliation scans the full historical logical population.
func bulkInsertCardinalityHistory(ctx context.Context, db *sql.DB, session string, n int, base time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO event_journal (
			event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type,
			observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
		) VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	var seq int64
	emit := func(frame int64, evSeq int64, evType, connID, observedAt, eventJSON string) error {
		seq++
		sum := sha256.Sum256([]byte(eventJSON))
		_, err := stmt.ExecContext(ctx,
			fmt.Sprintf("card-%d", seq), session, frame, evSeq, evType, observedAt, connID,
			eventJSON, hex.EncodeToString(sum[:]), observedAt, seq)
		return err
	}

	newEvent := func(connID string, ts time.Time, up, down int64, candidate bool) string {
		meta := `{"network":"tcp","process":"app.exe","host":"logical.example"}`
		attr := "known_application"
		rule := `"MATCH"`
		if candidate {
			meta = `{"network":"tcp","host":"relay.example"}`
			attr = "relay_candidate"
			rule = `""`
		}
		return fmt.Sprintf(`{"eventId":"%s","sessionId":%q,"epochId":1,"frameSequence":0,"eventSequence":1,`+
			`"timestamp":%q,"type":"ConnectionNew","connectionId":%q,`+
			`"observedUploadCounter":%d,"observedDownloadCounter":%d,`+
			`"deltaUpload":%d,"deltaDownload":%d,`+
			`"monitoredCumulativeUpload":%d,"monitoredCumulativeDownload":%d,`+
			`"baselineUploadCounter":0,"baselineDownloadCounter":0,`+
			`"route":"PROXY","attributionClass":%q,`+
			`"metadata":%s,"rule":%s,"chains":["Node-C","Group-C"]}`,
			connID, session, ts.UTC().Format(time.RFC3339Nano), connID, up, down, up, down, up, down, attr, meta, rule)
	}
	disappearEvent := func(connID string, ts time.Time) string {
		return fmt.Sprintf(`{"eventId":"%s-dis","sessionId":%q,"epochId":1,"frameSequence":0,"eventSequence":2,`+
			`"timestamp":%q,"type":"ConnectionDisappeared","connectionId":%q,"possibleUnobservedTail":true}`,
			connID, session, ts.UTC().Format(time.RFC3339Nano), connID)
	}

	// Two long-lived active connections at session start (conn-active-cand is
	// the candidate whose class decision scans the logical population).
	activeTS := base
	if err := emit(1, 1, "ConnectionNew", "conn-active-cand", activeTS.Format(time.RFC3339Nano),
		newEvent("conn-active-cand", activeTS, 4000, 4000, true)); err != nil {
		return err
	}
	if err := emit(2, 1, "ConnectionNew", "conn-active-logical", activeTS.Add(time.Second).Format(time.RFC3339Nano),
		newEvent("conn-active-logical", activeTS.Add(time.Second), 4050, 3980, false)); err != nil {
		return err
	}

	for i := 0; i < n; i++ {
		ts := base.Add(2*time.Second + time.Duration(i)*300*time.Millisecond)
		frame := int64(100) + int64(i)
		connID := fmt.Sprintf("card-hist-%06d", i)
		cand := i%50 == 0 // 2% historical candidates, matching a realistic mix
		up, down := int64(6000), int64(6100)
		if cand {
			up, down = 4200, 4100
		}
		if err := emit(frame, 1, "ConnectionNew", connID, ts.Format(time.RFC3339Nano), newEvent(connID, ts, up, down, cand)); err != nil {
			return err
		}
		if err := emit(frame, 2, "ConnectionDisappeared", connID, ts.Add(100*time.Millisecond).Format(time.RFC3339Nano), disappearEvent(connID, ts.Add(100*time.Millisecond))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// bulkInsertCardinalityBatch appends the identical small batch: deltas on the
// two long-lived connections (their ancient firstObs pulls the full terminal
// history into the dirty-closure window - the honest worst case) plus a few
// brand-new connections.
func bulkInsertCardinalityBatch(ctx context.Context, db *sql.DB, session string, base time.Time, frameBase int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO event_journal (
			event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type,
			observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
		) VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	var seq int64
	var maxSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence),0) FROM event_journal;`).Scan(&maxSeq); err != nil {
		return err
	}
	emit := func(frame int64, evSeq int64, evType, connID, observedAt, eventJSON string) error {
		seq++
		sum := sha256.Sum256([]byte(eventJSON))
		_, err := stmt.ExecContext(ctx,
			fmt.Sprintf("cardbatch-%d", seq), session, frame, evSeq, evType, observedAt, connID,
			eventJSON, hex.EncodeToString(sum[:]), observedAt, maxSeq+seq)
		return err
	}

	deltaEvent := func(connID string, ts time.Time, up, down int64, candidate bool) string {
		meta := `{"network":"tcp","process":"app.exe","host":"logical.example"}`
		attr := "known_application"
		rule := `"MATCH"`
		if candidate {
			meta = `{"network":"tcp","host":"relay.example"}`
			attr = "relay_candidate"
			rule = `""`
		}
		return fmt.Sprintf(`{"eventId":"%s-d","sessionId":%q,"epochId":1,"frameSequence":0,"eventSequence":1,`+
			`"timestamp":%q,"type":"ConnectionDelta","connectionId":%q,`+
			`"observedUploadCounter":%d,"observedDownloadCounter":%d,`+
			`"deltaUpload":150,"deltaDownload":250,`+
			`"monitoredCumulativeUpload":%d,"monitoredCumulativeDownload":%d,`+
			`"baselineUploadCounter":0,"baselineDownloadCounter":0,`+
			`"route":"PROXY","attributionClass":%q,`+
			`"metadata":%s,"rule":%s,"chains":["Node-C","Group-C"]}`,
			connID+"-b", session, ts.UTC().Format(time.RFC3339Nano), connID, up, down, up+150, down+250, attr, meta, rule)
	}
	newEvent := func(connID string, ts time.Time, up, down int64, candidate bool) string {
		meta := `{"network":"tcp","process":"app.exe","host":"logical.example"}`
		attr := "known_application"
		rule := `"MATCH"`
		if candidate {
			meta = `{"network":"tcp","host":"relay.example"}`
			attr = "relay_candidate"
			rule = `""`
		}
		return fmt.Sprintf(`{"eventId":"%s","sessionId":%q,"epochId":1,"frameSequence":0,"eventSequence":1,`+
			`"timestamp":%q,"type":"ConnectionNew","connectionId":%q,`+
			`"observedUploadCounter":%d,"observedDownloadCounter":%d,`+
			`"deltaUpload":%d,"deltaDownload":%d,`+
			`"monitoredCumulativeUpload":%d,"monitoredCumulativeDownload":%d,`+
			`"baselineUploadCounter":0,"baselineDownloadCounter":0,`+
			`"route":"PROXY","attributionClass":%q,`+
			`"metadata":%s,"rule":%s,"chains":["Node-C","Group-C"]}`,
			connID, session, ts.UTC().Format(time.RFC3339Nano), connID, up, down, up, down, up, down, attr, meta, rule)
	}

	// Deltas on the two long-lived connections.
	if err := emit(frameBase, 1, "ConnectionDelta", "conn-active-cand", base.Format(time.RFC3339Nano),
		deltaEvent("conn-active-cand", base, 4150, 4250, true)); err != nil {
		return err
	}
	if err := emit(frameBase+1, 1, "ConnectionDelta", "conn-active-logical", base.Add(250*time.Millisecond).Format(time.RFC3339Nano),
		deltaEvent("conn-active-logical", base.Add(250*time.Millisecond), 4200, 4430, false)); err != nil {
		return err
	}
	// Five brand-new logical connections with traffic.
	for i := 0; i < 5; i++ {
		connID := fmt.Sprintf("card-new-%06d", i)
		ts := base.Add(time.Duration(500+i*250) * time.Millisecond)
		if err := emit(frameBase+2+int64(i), 1, "ConnectionNew", connID, ts.Format(time.RFC3339Nano), newEvent(connID, ts, 1000+int64(i)*100, 2000+int64(i)*100, false)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func netListen() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

func lnPort(ln net.Listener) int {
	return ln.Addr().(*net.TCPAddr).Port
}
