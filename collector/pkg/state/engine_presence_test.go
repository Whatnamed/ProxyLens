package state

import (
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// frameAt builds a snapshot frame at an absolute offset with the given connections.
func frameAt(offset time.Duration, uploadTotal, downloadTotal int64, conns []types.ConnectionSnapshot) *types.ConnectionSnapshotFrame {
	base := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	return &types.ConnectionSnapshotFrame{
		ReceivedAt: base.Add(offset).Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   uploadTotal,
			DownloadTotal: downloadTotal,
			Connections:   conns,
		},
	}
}

func countEvents(events []*types.CollectorEvent, predicate func(*types.CollectorEvent) bool) int {
	n := 0
	for _, ev := range events {
		if predicate(ev) {
			n++
		}
	}
	return n
}

// TestZeroOnlyConnectionEmitsPresenceNotDelta proves the raw density contract:
// a long-lived connection that never transfers bytes and never changes metadata
// must produce zero durable ConnectionDelta rows while remaining durably
// observable through sparse presence checkpoints (one per 30s interval).
func TestZeroOnlyConnectionEmitsPresenceNotDelta(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	conn := func() []types.ConnectionSnapshot {
		return []types.ConnectionSnapshot{
			{
				ID:       "idle-1",
				Upload:   1000,
				Download: 2000,
				Metadata: types.RawMetadata{Process: "idle.exe", Host: "keepalive.example"},
				Rule:     "Proxy",
				Chains:   []string{"Node-1", "Group"},
			},
		}
	}

	const frameStep = 250 * time.Millisecond
	const totalFrames = 2401 // 10 minutes inclusive at 250ms cadence
	for i := 0; i < totalFrames; i++ {
		if i == 0 {
			if err := engine.ProcessFrame(frameAt(0, 3000, 6000, conn())); err != nil {
				t.Fatalf("bootstrap frame failed: %v", err)
			}
			continue
		}
		if err := engine.ProcessFrame(frameAt(time.Duration(i)*frameStep, 3000, 6000, conn())); err != nil {
			t.Fatalf("frame %d failed: %v", i, err)
		}
	}

	events := memSink.GetEvents()
	deltas := countEvents(events, func(ev *types.CollectorEvent) bool {
		return ev.Type == types.EventConnectionDelta && ev.ConnectionID == "idle-1"
	})
	presence := countEvents(events, func(ev *types.CollectorEvent) bool {
		return ev.Type == types.EventConnectionPresenceCheckpoint && ev.ConnectionID == "idle-1"
	})

	if deltas != 0 {
		t.Errorf("expected 0 ConnectionDelta for zero-only connection, got %d", deltas)
	}
	// One checkpoint at t=30s,60s,...,570s plus one at t=600s: the first frame
	// at or after each 30s boundary. 600s/30s = 20 expected checkpoints.
	if presence < 19 || presence > 21 {
		t.Errorf("expected ~20 presence checkpoints over 10 minutes, got %d", presence)
	}

	// Presence checkpoints must carry observed counters (liveness facts) but
	// zero deltas, and must be spaced by at least the presence interval.
	var lastPresence time.Time
	presenceCount := 0
	for _, ev := range events {
		if ev.Type != types.EventConnectionPresenceCheckpoint {
			continue
		}
		presenceCount++
		if ev.DeltaUpload != 0 || ev.DeltaDownload != 0 {
			t.Errorf("presence checkpoint must not carry traffic deltas")
		}
		if ev.ObservedUploadCounter != 1000 || ev.ObservedDownloadCounter != 2000 {
			t.Errorf("presence checkpoint must carry observed counters, got %d/%d", ev.ObservedUploadCounter, ev.ObservedDownloadCounter)
		}
		if !lastPresence.IsZero() && ev.Timestamp.Sub(lastPresence) < presenceCheckpointInterval {
			t.Errorf("presence checkpoints closer than %s apart: %s", presenceCheckpointInterval, ev.Timestamp.Sub(lastPresence))
		}
		lastPresence = ev.Timestamp
	}

	// Disappearance must close with the exact final known presence.
	if err := engine.ProcessFrame(frameAt(time.Duration(totalFrames)*frameStep, 3000, 6000, nil)); err != nil {
		t.Fatalf("disappearance frame failed: %v", err)
	}
	events = memSink.GetEvents()
	var disappeared *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionDisappeared && ev.ConnectionID == "idle-1" {
			disappeared = ev
			break
		}
	}
	if disappeared == nil {
		t.Fatalf("expected ConnectionDisappeared for idle-1")
	}
	lastSeen := disappeared.Details["lastObservedAt"]
	if lastSeen == nil {
		t.Fatalf("disappeared event must carry lastObservedAt detail")
	}
	// Final sighting was the last frame where the connection was present:
	// t = 600s (frames span 0..600s inclusive; the disappearance frame is at 600.25s).
	wantLast := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC).Add(600 * time.Second)
	gotLast, err := time.Parse(time.RFC3339Nano, lastSeen.(string))
	if err != nil {
		t.Fatalf("lastObservedAt not parseable: %v", err)
	}
	if !gotLast.Equal(wantLast) {
		t.Errorf("lastObservedAt = %s, want %s", gotLast.Format(time.RFC3339Nano), wantLast.Format(time.RFC3339Nano))
	}
	if disappeared.Timestamp.Equal(gotLast) {
		t.Errorf("disappeared_observed_at must be the first absent observation, not the last sighting")
	}
}

// TestMixedTrafficPreservesByteSums proves the suppression contract keeps byte
// accounting exactly identical to legacy behavior: every positive clamped
// delta is emitted exactly once with correct cumulative counters, and
// suppressing zero-byte frames cannot change any byte total.
func TestMixedTrafficPreservesByteSums(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	// Legacy reference: simulate the legacy emission semantics (emit a Delta
	// for every frame with clamped deltas, including zero) to compute the
	// expected byte sums and per-frame cumulative counters.
	type refState struct {
		lastUp, lastDown int64
		cumUp, cumDown   int64
		everNew          bool
	}
	ref := map[string]*refState{}
	var wantUp, wantDown int64
	wantCumByFrame := map[int64][2]int64{}

	type step struct {
		offset    time.Duration
		up        int64
		down      int64
		totalUp   int64
		totalDown int64
	}
	var connID = "mixed-1"
	var steps []step
	base := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	frameTimes := func(i int) time.Time { return base.Add(time.Duration(i) * 250 * time.Millisecond) }

	mkConn := func(up, down int64) []types.ConnectionSnapshot {
		return []types.ConnectionSnapshot{
			{
				ID:       connID,
				Upload:   up,
				Download: down,
				Metadata: types.RawMetadata{Process: "mix.exe", Host: "mixed.example"},
				Rule:     "Proxy",
				Chains:   []string{"Node-1"},
			},
		}
	}

	// Frame 0: bootstrap (preexisting connection, counters as baseline).
	steps = append(steps, step{offset: 0, up: 5000, down: 8000, totalUp: 10000, totalDown: 20000})
	// Idle frames with no bytes.
	for i := 1; i <= 10; i++ {
		steps = append(steps, step{offset: time.Duration(i) * 250 * time.Millisecond, up: 5000, down: 8000, totalUp: 10000, totalDown: 20000})
	}
	// Traffic burst.
	steps = append(steps, step{offset: 3 * time.Second, up: 9000, down: 30000, totalUp: 20000, totalDown: 60000})
	// More idle.
	for i := 13; i <= 20; i++ {
		steps = append(steps, step{offset: time.Duration(i) * 250 * time.Millisecond, up: 9000, down: 30000, totalUp: 20000, totalDown: 60000})
	}
	// Download-only traffic.
	steps = append(steps, step{offset: 5250 * time.Millisecond, up: 9000, down: 40000, totalUp: 20000, totalDown: 80000})
	// Idle again.
	for i := 22; i <= 30; i++ {
		steps = append(steps, step{offset: time.Duration(i) * 250 * time.Millisecond, up: 9000, down: 40000, totalUp: 20000, totalDown: 80000})
	}

	for i, s := range steps {
		frame := &types.ConnectionSnapshotFrame{
			ReceivedAt: frameTimes(i).Format(time.RFC3339Nano),
			Frame: types.ConnectionSnapshotPayload{
				UploadTotal:   s.totalUp,
				DownloadTotal: s.totalDown,
				Connections:   mkConn(s.up, s.down),
			},
		}
		if err := engine.ProcessFrame(frame); err != nil {
			t.Fatalf("frame %d failed: %v", i, err)
		}

		// Legacy reference computation.
		rs := ref[connID]
		if rs == nil {
			rs = &refState{}
			ref[connID] = rs
		}
		if !rs.everNew && i == 0 {
			// Bootstrap: baseline only, no deltas.
			rs.lastUp, rs.lastDown = s.up, s.down
			rs.everNew = true
			wantCumByFrame[int64(i+1)] = [2]int64{0, 0}
			continue
		}
		if !rs.everNew {
			// First sight in steady state: ConnectionNew carries full counters.
			rs.lastUp, rs.lastDown = s.up, s.down
			rs.everNew = true
			wantUp += s.up
			wantDown += s.down
			rs.cumUp, rs.cumDown = s.up, s.down
			wantCumByFrame[int64(i+1)] = [2]int64{rs.cumUp, rs.cumDown}
			continue
		}
		dUp := s.up - rs.lastUp
		dDown := s.down - rs.lastDown
		if dUp < 0 {
			dUp = 0
		}
		if dDown < 0 {
			dDown = 0
		}
		rs.lastUp, rs.lastDown = s.up, s.down
		rs.cumUp += dUp
		rs.cumDown += dDown
		wantUp += dUp
		wantDown += dDown
		wantCumByFrame[int64(i+1)] = [2]int64{rs.cumUp, rs.cumDown}
	}

	events := memSink.GetEvents()
	var gotUp, gotDown int64
	for _, ev := range events {
		if ev.Type == types.EventConnectionDelta && ev.ConnectionID == connID {
			gotUp += ev.DeltaUpload
			gotDown += ev.DeltaDownload
		}
	}
	if gotUp != wantUp || gotDown != wantDown {
		t.Errorf("byte sums diverge from legacy: got up=%d down=%d, want up=%d down=%d", gotUp, wantUp, gotDown, wantDown)
	}

	// Every emitted Delta must carry the same per-frame cumulative counters
	// legacy would have computed at that point.
	for _, ev := range events {
		if ev.Type != types.EventConnectionDelta || ev.ConnectionID != connID {
			continue
		}
		want, ok := wantCumByFrame[ev.FrameSequence]
		if !ok {
			t.Fatalf("no reference cumulative for frame %d", ev.FrameSequence)
		}
		if ev.MonitoredCumulativeUpload != want[0] || ev.MonitoredCumulativeDownload != want[1] {
			t.Errorf("cumulative counters diverge at frame %d: got %d/%d want %d/%d",
				ev.FrameSequence, ev.MonitoredCumulativeUpload, ev.MonitoredCumulativeDownload, want[0], want[1])
			break
		}
	}

	// No presence checkpoint may share a frame with any ConnectionDelta.
	deltaFrames := map[int64]bool{}
	for _, ev := range events {
		if ev.Type == types.EventConnectionDelta && ev.ConnectionID == connID {
			deltaFrames[ev.FrameSequence] = true
		}
	}
	for _, ev := range events {
		if ev.Type == types.EventConnectionPresenceCheckpoint && ev.ConnectionID == connID && deltaFrames[ev.FrameSequence] {
			t.Errorf("presence checkpoint emitted at the same frame as a ConnectionDelta (frame %d)", ev.FrameSequence)
		}
	}
}

// TestReconnectZeroDeltaUsesPresenceContract proves the reconnect-recovery
// path applies the identical suppression contract: a zero-byte recovery frame
// emits presence (not an interval Delta), while a traffic recovery frame still
// emits the full interval_only ConnectionDelta.
func TestReconnectZeroDeltaUsesPresenceContract(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	connA := func(up, down int64) []types.ConnectionSnapshot {
		return []types.ConnectionSnapshot{
			{
				ID:       "reconn-a",
				Upload:   up,
				Download: down,
				Metadata: types.RawMetadata{Process: "a.exe", Host: "a.example"},
				Rule:     "Proxy",
				Chains:   []string{"Node-1", "Group"},
			},
			{
				ID:       "reconn-b",
				Upload:   up,
				Download: down,
				Metadata: types.RawMetadata{Process: "b.exe", Host: "b.example"},
				Rule:     "Proxy",
				Chains:   []string{"Node-1", "Group"},
			},
		}
	}

	if err := engine.ProcessFrame(frameAt(0, 1000, 2000, connA(100, 200))); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if err := engine.ProcessFrame(frameAt(250*time.Millisecond, 1000, 2000, connA(100, 200))); err != nil {
		t.Fatalf("steady frame failed: %v", err)
	}

	// Simulate a controller gap via the ingest item path.
	if err := engine.ProcessIngestItem(&types.IngestItem{
		Kind:      types.ItemGapOpened,
		Timestamp: time.Date(2026, 9, 5, 0, 0, 10, 0, time.UTC),
	}); err != nil {
		t.Fatalf("gap open failed: %v", err)
	}

	// Recovery frame: connection A reappears with unchanged counters
	// (zero delta across the gap); connection B reappears with traffic.
	recovery := []types.ConnectionSnapshot{
		{
			ID:       "reconn-a",
			Upload:   100,
			Download: 200,
			Metadata: types.RawMetadata{Process: "a.exe", Host: "a.example"},
			Rule:     "Proxy",
			Chains:   []string{"Node-1", "Group"},
		},
		{
			ID:       "reconn-b",
			Upload:   5000,
			Download: 9000,
			Metadata: types.RawMetadata{Process: "b.exe", Host: "b.example"},
			Rule:     "Proxy",
			Chains:   []string{"Node-1", "Group"},
		},
	}
	recoveryTs := time.Date(2026, 9, 5, 0, 1, 0, 0, time.UTC)
	if err := engine.ProcessIngestItem(&types.IngestItem{
		Kind:      types.ItemFrame,
		Timestamp: recoveryTs,
		Frame: &types.ConnectionSnapshotFrame{
			ReceivedAt: recoveryTs.Format(time.RFC3339Nano),
			Frame: types.ConnectionSnapshotPayload{
				UploadTotal:   3000,
				DownloadTotal: 6000,
				Connections:   recovery,
			},
		},
	}); err != nil {
		t.Fatalf("recovery frame failed: %v", err)
	}

	events := memSink.GetEvents()
	aDelta := false
	bDelta := false
	aPresence := false
	for _, ev := range events {
		if ev.Type == types.EventConnectionDelta && ev.ConnectionID == "reconn-a" {
			aDelta = true
		}
		if ev.Type == types.EventConnectionPresenceCheckpoint && ev.ConnectionID == "reconn-a" {
			aPresence = true
		}
		if ev.Type == types.EventConnectionDelta && ev.ConnectionID == "reconn-b" {
			bDelta = true
			if ev.Precision != "interval_only" {
				t.Errorf("traffic recovery delta must keep interval_only precision, got %q", ev.Precision)
			}
			if ev.DeltaUpload != 4900 || ev.DeltaDownload != 8800 {
				t.Errorf("traffic recovery delta bytes wrong: %d/%d", ev.DeltaUpload, ev.DeltaDownload)
			}
		}
	}
	if aDelta {
		t.Errorf("zero-byte recovery must not emit ConnectionDelta")
	}
	if !aPresence {
		t.Errorf("zero-byte recovery must emit a presence checkpoint for liveness")
	}
	if !bDelta {
		t.Errorf("traffic recovery must emit ConnectionDelta")
	}
}

// TestPresenceTimerResetByMetadataUpdates proves metadata/relay/health durable
// evidence resets the presence cadence so a connection with frequent metadata
// updates does not accumulate redundant presence checkpoints.
func TestPresenceTimerResetByMetadataUpdates(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	mk := func(rule string) []types.ConnectionSnapshot {
		return []types.ConnectionSnapshot{
			{
				ID:       "meta-1",
				Upload:   100,
				Download: 200,
				Metadata: types.RawMetadata{Process: "m.exe", Host: "m.example"},
				Rule:     rule,
				Chains:   []string{"Node-1"},
			},
		}
	}

	if err := engine.ProcessFrame(frameAt(0, 1000, 2000, mk("Rule-A"))); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	// Every 20s the rule changes, which is durable MetadataUpdated evidence;
	// no presence checkpoint should ever be needed.
	for i := 1; i <= 9; i++ {
		rule := "Rule-A"
		if i%2 == 0 {
			rule = "Rule-B"
		}
		if err := engine.ProcessFrame(frameAt(time.Duration(i)*20*time.Second, 1000, 2000, mk(rule))); err != nil {
			t.Fatalf("frame %d failed: %v", i, err)
		}
	}

	events := memSink.GetEvents()
	presence := countEvents(events, func(ev *types.CollectorEvent) bool {
		return ev.Type == types.EventConnectionPresenceCheckpoint
	})
	meta := countEvents(events, func(ev *types.CollectorEvent) bool {
		return ev.Type == types.EventConnectionMetadataUpdated
	})
	if presence != 0 {
		t.Errorf("metadata updates at 20s cadence must suppress presence checkpoints, got %d", presence)
	}
	if meta == 0 {
		t.Errorf("expected MetadataUpdated events for rule changes")
	}
}
