package storage

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// TestStoragePresenceCheckpointProjectsLivenessOnly proves the durable
// presence contract: presence checkpoints refresh connections liveness facts
// without creating connection_traffic rows, and Disappeared events project the
// exact final known presence carried from engine memory.
func TestStoragePresenceCheckpointProjectsLivenessOnly(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	s, err := OpenSQLiteSink(ctx, dbPath, "sess-presence", "v1.0.0-test")
	if err != nil {
		t.Fatalf("failed to open sqlite sink: %v", err)
	}

	base := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	mk := func(seq int64, evSeq int64, ts time.Time, evType types.EventType, details map[string]any) *types.CollectorEvent {
		return &types.CollectorEvent{
			EventID:                 fmtEventID("presence-test", seq, evSeq, evType),
			SessionID:               "sess-presence",
			EpochID:                 1,
			FrameSequence:           seq,
			EventSequence:           evSeq,
			Type:                    evType,
			Timestamp:               ts,
			ConnectionID:            "idle-1",
			ObservedUploadCounter:   1000,
			ObservedDownloadCounter: 2000,
			Route:                   types.RouteProxy,
			AttributionClass:        types.ClassKnownApplication,
			Metadata:                types.RawMetadata{Process: "idle.exe", Host: "keepalive.example"},
			Rule:                    "Proxy",
			Chains:                  []string{"Node-1", "Group"},
			Precision:               "presence_checkpoint",
			Details:                 details,
		}
	}

	// New event: creates the connection and one traffic row.
	if err := s.Emit(mk(1, 1, base, types.EventConnectionNew, nil)); err != nil {
		t.Fatalf("emit New failed: %v", err)
	}

	// Presence checkpoint 30s later: liveness facts only.
	presenceAt := base.Add(30 * time.Second)
	if err := s.Emit(mk(121, 1, presenceAt, types.EventConnectionPresenceCheckpoint, nil)); err != nil {
		t.Fatalf("emit presence failed: %v", err)
	}

	// Disappeared 20s after the last presence with exact final sighting
	// details (the engine saw the connection 5s after the last checkpoint).
	lastSighting := presenceAt.Add(5 * time.Second)
	disappearedAt := lastSighting.Add(15 * time.Second)
	if err := s.Emit(mk(200, 1, disappearedAt, types.EventConnectionDisappeared, map[string]any{
		"lastObservedAt":              lastSighting.Format(time.RFC3339Nano),
		"lastObservedUploadCounter":   1500,
		"lastObservedDownloadCounter": 2600,
	})); err != nil {
		t.Fatalf("emit disappeared failed: %v", err)
	}

	db, err := OpenReadOnlyDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen db: %v", err)
	}
	defer db.Close()

	var trafficRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_traffic WHERE session_id = 'sess-presence';`).Scan(&trafficRows); err != nil {
		t.Fatalf("failed to count traffic rows: %v", err)
	}
	if trafficRows != 1 {
		t.Errorf("presence checkpoint must not create connection_traffic rows; got %d rows (want 1 from ConnectionNew)", trafficRows)
	}

	var journalPresence int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE event_type = 'ConnectionPresenceCheckpoint';`).Scan(&journalPresence); err != nil {
		t.Fatalf("failed to count presence journal rows: %v", err)
	}
	if journalPresence != 1 {
		t.Errorf("expected 1 presence checkpoint in journal, got %d", journalPresence)
	}

	var lastObservedStr string
	var lastUp, lastDown int64
	var disappearedStr string
	if err := db.QueryRowContext(ctx, `
		SELECT last_observed_at, last_observed_upload_counter, last_observed_download_counter, disappeared_observed_at
		FROM connections WHERE session_id = 'sess-presence' AND connection_id = 'idle-1';
	`).Scan(&lastObservedStr, &lastUp, &lastDown, &disappearedStr); err != nil {
		t.Fatalf("failed to read connection row: %v", err)
	}

	if got, _ := time.Parse(time.RFC3339Nano, lastObservedStr); !got.Equal(lastSighting.UTC()) {
		t.Errorf("last_observed_at = %s, want final sighting %s", lastObservedStr, lastSighting.UTC().Format(time.RFC3339Nano))
	}
	if lastUp != 1500 || lastDown != 2600 {
		t.Errorf("last observed counters = %d/%d, want 1500/2600", lastUp, lastDown)
	}
	if got, _ := time.Parse(time.RFC3339Nano, disappearedStr); !got.Equal(disappearedAt.UTC()) {
		t.Errorf("disappeared_observed_at = %s, want first absent observation %s", disappearedStr, disappearedAt.UTC().Format(time.RFC3339Nano))
	}
}

func fmtEventID(session string, seq, evSeq int64, evType types.EventType) string {
	return session + "-" + string(evType) + "-" + strconv.FormatInt(seq, 10) + "-" + strconv.FormatInt(evSeq, 10)
}
