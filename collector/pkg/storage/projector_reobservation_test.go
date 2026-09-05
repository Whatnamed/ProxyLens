package storage

import (
	"context"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// TestStorageConnectionReobservationReopensRow proves the same-epoch
// re-observation contract: a connection ID observed again after a
// Disappeared within the same session+epoch must reopen the existing
// connections row (snapshot flapping or ID reuse) instead of violating the
// (session_id, epoch_id, connection_id) uniqueness with a second row. The
// journal keeps both the Disappeared and the second New event as raw
// evidence; the projection is deterministic under RebuildProjections replay.
func TestStorageConnectionReobservationReopensRow(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	s, err := OpenSQLiteSink(ctx, dbPath, "sess-reobs", "v1.0.0-test")
	if err != nil {
		t.Fatalf("failed to open sqlite sink: %v", err)
	}

	base := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	mihomoStart := base.Add(-1 * time.Minute)
	mk := func(seq int64, ts time.Time, evType types.EventType, up, down, dUp, dDown, cumUp, cumDown int64) *types.CollectorEvent {
		return &types.CollectorEvent{
			EventID:                     fmtEventID("reobs-test", seq, 1, evType),
			SessionID:                   "sess-reobs",
			EpochID:                     1,
			FrameSequence:               seq,
			EventSequence:               1,
			Type:                        evType,
			Timestamp:                   ts,
			ConnectionID:                "churn-1",
			MihomoStart:                 mihomoStart.Format(time.RFC3339Nano),
			ObservedUploadCounter:       up,
			ObservedDownloadCounter:     down,
			DeltaUpload:                 dUp,
			DeltaDownload:               dDown,
			MonitoredCumulativeUpload:   cumUp,
			MonitoredCumulativeDownload: cumDown,
			BaselineUploadCounter:       up - dUp,
			BaselineDownloadCounter:     down - dDown,
			Route:                       types.RouteProxy,
			AttributionClass:            types.ClassKnownApplication,
			Metadata:                    types.RawMetadata{Process: "churn.exe", Host: "reobs.example", Network: "tcp"},
			Rule:                        "Proxy",
			Chains:                      []string{"Node-1", "Group"},
		}
	}

	// First observation window: New, one traffic delta, then Disappeared.
	if err := s.Emit(mk(1, base, types.EventConnectionNew, 100, 200, 0, 0, 0, 0)); err != nil {
		t.Fatalf("first New failed: %v", err)
	}
	if err := s.Emit(mk(2, base.Add(10*time.Second), types.EventConnectionDelta, 300, 600, 200, 400, 200, 400)); err != nil {
		t.Fatalf("delta failed: %v", err)
	}
	disappearedAt := base.Add(20 * time.Second)
	if err := s.Emit(mk(3, disappearedAt, types.EventConnectionDisappeared, 300, 600, 0, 0, 200, 400)); err != nil {
		t.Fatalf("disappeared failed: %v", err)
	}

	// Same-epoch re-observation 10s later: fresh baseline, cumulative reset.
	reobservedAt := base.Add(30 * time.Second)
	if err := s.Emit(mk(4, reobservedAt, types.EventConnectionNew, 350, 650, 0, 0, 0, 0)); err != nil {
		t.Fatalf("re-observation New failed (must not hit UNIQUE constraint): %v", err)
	}

	db, err := OpenReadOnlyDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen db: %v", err)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connections WHERE session_id='sess-reobs' AND connection_id='churn-1';`).Scan(&rowCount); err != nil {
		t.Fatalf("failed to count connection rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("re-observation must reuse the existing row; got %d rows", rowCount)
	}

	var state, firstObsStr, endedStr, disappearedStr string
	var baselineUp, baselineDown, monUp, monDown int64
	if err := db.QueryRowContext(ctx, `
		SELECT state, first_observed_at, COALESCE(observation_ended_at, ''), COALESCE(disappeared_observed_at, ''),
		       baseline_upload_counter, baseline_download_counter, monitored_upload_total, monitored_download_total
		FROM connections WHERE session_id='sess-reobs' AND connection_id='churn-1';
	`).Scan(&state, &firstObsStr, &endedStr, &disappearedStr, &baselineUp, &baselineDown, &monUp, &monDown); err != nil {
		t.Fatalf("failed to read reobserved row: %v", err)
	}
	if state != "active" {
		t.Errorf("re-observed row state = %q, want active", state)
	}
	if got, _ := time.Parse(time.RFC3339Nano, firstObsStr); !got.Equal(base.UTC()) {
		t.Errorf("first_observed_at = %s, want original first observation %s", firstObsStr, base.UTC().Format(time.RFC3339Nano))
	}
	if endedStr != "" {
		t.Errorf("observation_ended_at = %q, want NULL after re-observation", endedStr)
	}
	if disappearedStr != "" {
		t.Errorf("disappeared_observed_at = %q, want NULL after re-observation", disappearedStr)
	}
	if baselineUp != 350 || baselineDown != 650 {
		t.Errorf("baseline counters = %d/%d, want fresh re-observation baseline 350/650", baselineUp, baselineDown)
	}
	if monUp != 0 || monDown != 0 {
		t.Errorf("monitored totals = %d/%d, want reset for fresh observation window 0/0", monUp, monDown)
	}

	var journalCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE session_id='sess-reobs';`).Scan(&journalCount); err != nil {
		t.Fatalf("failed to count journal rows: %v", err)
	}
	if journalCount != 4 {
		t.Errorf("journal must keep all 4 events (New/Delta/Disappeared/New), got %d", journalCount)
	}
	_ = db.Close()

	// Replay determinism: a full projection rebuild over the same journal must
	// produce the identical reopened row.
	wdb, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to open writer db for rebuild: %v", err)
	}
	if err := RebuildProjections(ctx, wdb); err != nil {
		t.Fatalf("rebuild projections failed: %v", err)
	}
	_ = wdb.Close()

	db2, err := OpenReadOnlyDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen db after rebuild: %v", err)
	}
	defer db2.Close()

	var state2, firstObs2, ended2 string
	if err := db2.QueryRowContext(ctx, `
		SELECT state, first_observed_at, COALESCE(observation_ended_at, '')
		FROM connections WHERE session_id='sess-reobs' AND connection_id='churn-1';
	`).Scan(&state2, &firstObs2, &ended2); err != nil {
		t.Fatalf("failed to read row after rebuild: %v", err)
	}
	if state2 != "active" || firstObs2 != firstObsStr || ended2 != "" {
		t.Errorf("rebuild replay diverged: state=%q first_observed=%q (want %q) ended=%q", state2, firstObs2, firstObsStr, ended2)
	}
}
