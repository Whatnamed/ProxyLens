package storage

import (
	"context"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// TestStateEngineReobservationNoDoubleAccounting drives the real production
// pipeline StateEngine -> SQLiteEventSink -> journal/projection -> accounting
// over a snapshot flap: the same Mihomo connection (same ID + same Mihomo
// start) disappears and reappears with continuing counters. The accounted
// bytes must only cover the counter difference (+50/+50), never the already
// monitored lifetime counters (+350/+650).
func TestStateEngineReobservationNoDoubleAccounting(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-reobs", "test")
	if err != nil {
		t.Fatalf("open sink: %v", err)
	}
	engine := state.NewStateEngine(state.EngineOptions{Sink: sink, SessionID: "sess-reobs"})

	const mihomoStart = "2026-09-05T09:00:00Z"
	base := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

	frame := func(i int, conns []types.ConnectionSnapshot, totalUp, totalDown int64) error {
		return engine.ProcessFrame(&types.ConnectionSnapshotFrame{
			ReceivedAt: base.Add(time.Duration(i) * 250 * time.Millisecond).Format(time.RFC3339Nano),
			Frame: types.ConnectionSnapshotPayload{
				UploadTotal:   totalUp,
				DownloadTotal: totalDown,
				Connections:   conns,
			},
		})
	}
	reobsConn := func(up, down int64) []types.ConnectionSnapshot {
		return []types.ConnectionSnapshot{{
			ID: "re-1", Start: mihomoStart, Upload: up, Download: down,
			Metadata: types.RawMetadata{Host: "reobs.example", Network: "tcp"},
			Chains:   []string{"Node-R", "Group-R"},
		}}
	}

	// f0: bootstrap with no connections so the engine goes healthy.
	if err := frame(0, nil, 1000, 2000); err != nil {
		t.Fatalf("bootstrap frame: %v", err)
	}
	// f1: connection first observed mid-flight at lifetime counters 300/600.
	if err := frame(1, reobsConn(300, 600), 1300, 2600); err != nil {
		t.Fatalf("new frame: %v", err)
	}
	// f2: snapshot flap - connection disappears.
	if err := frame(2, nil, 1300, 2600); err != nil {
		t.Fatalf("disappear frame: %v", err)
	}
	// f3: same connection reappears with continuing counters 350/650.
	if err := frame(3, reobsConn(350, 650), 1350, 2650); err != nil {
		t.Fatalf("reappear frame: %v", err)
	}
	// f4: connection disappears for good.
	if err := frame(4, nil, 1350, 2650); err != nil {
		t.Fatalf("final disappear frame: %v", err)
	}

	if err := sink.EndSession(ctx, "sess-reobs", SessionStatusClosedClean); err != nil {
		t.Fatalf("end session: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close sink: %v", err)
	}

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Raw journal evidence: both incarnations of the observation lifecycle are
	// preserved verbatim (2 New + 2 Disappeared).
	var newEvents, disappearedEvents int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE event_type='ConnectionNew';`).Scan(&newEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE event_type='ConnectionDisappeared';`).Scan(&disappearedEvents); err != nil {
		t.Fatal(err)
	}
	if newEvents != 2 || disappearedEvents != 2 {
		t.Fatalf("expected 2 New + 2 Disappeared journal events, got %d/%d", newEvents, disappearedEvents)
	}

	if _, err := AdvanceAccountingV2(ctx, db, "reobservation regression", 0); err != nil {
		t.Fatalf("accounting v2: %v", err)
	}

	// Accounted bytes must equal the counter continuation exactly: the pre-flap
	// 300/600 plus 50/50 after reappearance. Double counting would total
	// 650/1250.
	var accUp, accDown int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(accounted_upload),0), COALESCE(SUM(accounted_download),0)
		FROM accounted_traffic_v2 WHERE connection_id='re-1';`).Scan(&accUp, &accDown); err != nil {
		t.Fatal(err)
	}
	if accUp != 350 || accDown != 650 {
		t.Fatalf("accounted bytes %d/%d, want 350/650 (re-observation double counting)", accUp, accDown)
	}
	var maxRawUp, maxRawDown int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(raw_upload),0), COALESCE(MAX(raw_download),0)
		FROM accounted_traffic_v2 WHERE connection_id='re-1';`).Scan(&maxRawUp, &maxRawDown); err != nil {
		t.Fatal(err)
	}
	if maxRawUp > 300 || maxRawDown > 600 {
		t.Fatalf("single accounted row carries lifetime counters: max raw %d/%d", maxRawUp, maxRawDown)
	}

	// Projections: one row for the whole observation lifecycle, first
	// observation preserved, counters continuing across the flap.
	var firstObs, stateStr, mihomoStartCol string
	var monUp, monDown int64
	if err := db.QueryRowContext(ctx, `
		SELECT first_observed_at, state, mihomo_start,
		       monitored_upload_total, monitored_download_total
		FROM connections WHERE session_id='sess-reobs' AND epoch_id=1 AND connection_id='re-1';`,
	).Scan(&firstObs, &stateStr, &mihomoStartCol, &monUp, &monDown); err != nil {
		t.Fatalf("connections row: %v", err)
	}
	wantFirstObs := base.Add(250 * time.Millisecond).UTC().Format(time.RFC3339Nano)
	if firstObs != wantFirstObs {
		t.Fatalf("first_observed_at %s, want original first observation %s", firstObs, wantFirstObs)
	}
	if stateStr != "disappeared_from_snapshot" {
		t.Fatalf("connection state %q, want terminal disappearance", stateStr)
	}
	if mihomoStartCol != mihomoStart {
		t.Fatalf("mihomo_start %q, want %q", mihomoStartCol, mihomoStart)
	}
	if monUp != 350 || monDown != 650 {
		t.Fatalf("projected monitored totals %d/%d, want continuation 350/650", monUp, monDown)
	}

	// v2 accounting state: totals continue, terminal marker from the final
	// disappearance.
	var stMonUp, stMonDown int64
	var disappearedAt string
	if err := db.QueryRowContext(ctx, `
		SELECT monitored_upload, monitored_download, disappeared_at
		FROM accounting_conn_state_v2
		WHERE session_id='sess-reobs' AND epoch_id=1 AND connection_id='re-1';`,
	).Scan(&stMonUp, &stMonDown, &disappearedAt); err != nil {
		t.Fatalf("conn state v2: %v", err)
	}
	if stMonUp != 350 || stMonDown != 650 {
		t.Fatalf("v2 monitored totals %d/%d, want 350/650", stMonUp, stMonDown)
	}
	if disappearedAt == "" {
		t.Fatalf("v2 lifecycle must be terminal after final disappearance")
	}

	// The re-observation New event carries the flap window as interval evidence.
	var reobsInterval string
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(interval_start,'') FROM connection_traffic
		WHERE connection_id='re-1' AND delta_upload=50;`).Scan(&reobsInterval); err != nil {
		t.Fatalf("re-observation interval evidence: %v", err)
	}
	if reobsInterval == "" {
		t.Fatalf("re-observation New event must carry its observation interval")
	}
}

// TestStateEngineReobservationDifferentStartIsNewIncarnation pins the other
// side of the contract: same connection ID with a DIFFERENT Mihomo start is a
// genuinely new connection, not a silent lifecycle merge.
func TestStateEngineReobservationDifferentStartIsNewIncarnation(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-reuse", "test")
	if err != nil {
		t.Fatalf("open sink: %v", err)
	}
	engine := state.NewStateEngine(state.EngineOptions{Sink: sink, SessionID: "sess-reuse"})

	base := time.Date(2026, 9, 5, 11, 0, 0, 0, time.UTC)
	frame := func(i int, start string, up, down int64, present bool) error {
		var conns []types.ConnectionSnapshot
		if present {
			conns = []types.ConnectionSnapshot{{
				ID: "reuse-1", Start: start, Upload: up, Download: down,
				Metadata: types.RawMetadata{Host: "reuse.example", Network: "tcp"},
				Chains:   []string{"Node-U", "Group-U"},
			}}
		}
		totalUp, totalDown := int64(1000), int64(2000)
		if present {
			totalUp += up
			totalDown += down
		}
		return engine.ProcessFrame(&types.ConnectionSnapshotFrame{
			ReceivedAt: base.Add(time.Duration(i) * 250 * time.Millisecond).Format(time.RFC3339Nano),
			Frame: types.ConnectionSnapshotPayload{
				UploadTotal: totalUp, DownloadTotal: totalDown, Connections: conns,
			},
		})
	}

	if err := frame(0, "", 0, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := frame(1, "2026-09-05T10:30:00Z", 300, 600, true); err != nil {
		t.Fatal(err)
	}
	if err := frame(2, "", 0, 0, false); err != nil {
		t.Fatal(err)
	}
	// Same ID reused by a different Mihomo connection (new start): must be
	// accounted as a new connection's full observed counters.
	if err := frame(3, "2026-09-05T11:00:00Z", 350, 650, true); err != nil {
		t.Fatal(err)
	}

	if err := sink.EndSession(ctx, "sess-reuse", SessionStatusClosedClean); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := AdvanceAccountingV2(ctx, db, "id reuse regression", 0); err != nil {
		t.Fatal(err)
	}
	var accUp, accDown int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(accounted_upload),0), COALESCE(SUM(accounted_download),0)
		FROM accounted_traffic_v2 WHERE connection_id='reuse-1';`).Scan(&accUp, &accDown); err != nil {
		t.Fatal(err)
	}
	// New incarnation: 300/600 from the first lifecycle plus 350/650 observed
	// for the genuinely new connection.
	if accUp != 650 || accDown != 1250 {
		t.Fatalf("accounted bytes %d/%d, want new-incarnation totals 650/1250", accUp, accDown)
	}
}
