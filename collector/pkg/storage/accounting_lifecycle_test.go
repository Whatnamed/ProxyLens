package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// -----------------------------------------------------------------------------
// Relay temporal overlap and lifecycle contract regressions (Phase 3S
// correctness closure). The candidate/logical fixture mirrors the review
// scenario: candidate A lives 00:00-00:05 and disappears; logical B lives
// 01:00-01:05 with the same physical chain and near-identical traffic totals.
// A terminal connection's observation window must never extend toward the
// boundary, so the pair must NOT classify as a relay duplicate.
// -----------------------------------------------------------------------------

const overlapSession = "sess-overlap"

var overlapBase = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

type overlapEmitter struct {
	t       *testing.T
	s       *SQLiteEventSink
	session string
	frame   int64
	seq     int64
}

func (e *overlapEmitter) emit(ts time.Time, ev *types.CollectorEvent) {
	e.t.Helper()
	e.seq++
	ev.SessionID = e.session
	ev.EpochID = 1
	ev.FrameSequence = e.frame
	ev.EventSequence = e.seq
	if ev.Timestamp.IsZero() {
		ev.Timestamp = ts
	}
	ev.GenerateDeterministicEventID()
	if err := e.s.Emit(ev); err != nil {
		e.t.Fatalf("emit failed: %v", err)
	}
}

func (e *overlapEmitter) nextFrame() time.Time {
	e.frame++
	return overlapBase.Add(time.Duration(e.frame) * time.Second)
}

func overlapCandidate(connID string, up, down int64) *types.CollectorEvent {
	return &types.CollectorEvent{
		Type: types.EventConnectionNew, ConnectionID: connID,
		DeltaUpload: up, DeltaDownload: down,
		ObservedUploadCounter: up, ObservedDownloadCounter: down,
		MonitoredCumulativeUpload: up, MonitoredCumulativeDownload: down,
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
		Metadata: types.RawMetadata{Host: "relay.example", Network: "tcp"},
		Chains:   []string{"NodeA", "GroupX"},
	}
}

func overlapLogical(connID string, up, down int64) *types.CollectorEvent {
	return &types.CollectorEvent{
		Type: types.EventConnectionNew, ConnectionID: connID,
		DeltaUpload: up, DeltaDownload: down,
		ObservedUploadCounter: up, ObservedDownloadCounter: down,
		MonitoredCumulativeUpload: up, MonitoredCumulativeDownload: down,
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "app.exe", Host: "logical.example", Network: "tcp"},
		Rule:     "MATCH",
		Chains:   []string{"NodeA", "GroupX"},
	}
}

func overlapDisappeared(connID string) *types.CollectorEvent {
	return &types.CollectorEvent{
		Type: types.EventConnectionDisappeared, ConnectionID: connID,
		PossibleUnobservedTail: true,
	}
}

// checkOverlapOutcome verifies the candidate stayed unpaired (missing
// attribution) and the logical stayed unique.
func checkOverlapOutcome(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var status string
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM relay_relations_v2 WHERE status='confirmed';`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("terminal candidate must not confirm as relay duplicate: %d confirmed relations", n)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT status FROM relay_relations_v2 WHERE candidate_connection_id='cand-A';`).Scan(&status); err != nil {
		t.Fatalf("candidate relation missing: %v", err)
	}
	if status != string(RelayUnpaired) {
		t.Fatalf("candidate relation status %q, want unpaired", status)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM accounting_conn_state_v2
		WHERE connection_id='cand-A' AND accounting_class != 'missing_attribution';`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("terminal candidate class must remain missing_attribution")
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM accounting_conn_state_v2
		WHERE connection_id='logical-B' AND accounting_class != 'unique';`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("logical class must remain unique")
	}
}

// TestRelayTemporalOverlapRespectsDisappearance seeds the whole closed
// history in one generation: A's disappearance at 00:05 must bound its
// overlap window, so the 01:00-01:05 logical B never pairs with it.
func TestRelayTemporalOverlapRespectsDisappearance(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	s, err := OpenSQLiteSink(ctx, dbPath, overlapSession, "test")
	if err != nil {
		t.Fatal(err)
	}
	em := &overlapEmitter{t: t, s: s, session: overlapSession}

	// Candidate A: 00:00-00:05, then disappeared.
	ts := em.nextFrame()
	em.emit(ts, overlapCandidate("cand-A", 5000, 5000))
	ts = em.nextFrame()
	em.emit(ts, overlapDisappeared("cand-A"))

	// Logical B: 01:00-01:05, same physical chain, near-identical totals.
	for i := 0; i < 55; i++ {
		em.nextFrame()
	}
	ts = em.nextFrame()
	em.emit(ts, overlapLogical("logical-B", 5050, 4980))
	ts = em.nextFrame()
	em.emit(ts, overlapDisappeared("logical-B"))

	if err := s.EndSession(ctx, overlapSession, SessionStatusClosedClean); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := AdvanceAccountingV2(ctx, db, "temporal overlap seed", 0); err != nil {
		t.Fatal(err)
	}
	checkOverlapOutcome(t, ctx, db)
}

// TestRelayTemporalOverlapIncrementalRespectsDisappearance runs the same
// scenario through seed-then-incremental: B arrives in a later chunk, and the
// incremental reclassification must not extend the terminal candidate's
// window to the new boundary.
func TestRelayTemporalOverlapIncrementalRespectsDisappearance(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	s, err := OpenSQLiteSink(ctx, dbPath, overlapSession, "test")
	if err != nil {
		t.Fatal(err)
	}
	em := &overlapEmitter{t: t, s: s, session: overlapSession}

	ts := em.nextFrame()
	em.emit(ts, overlapCandidate("cand-A", 5000, 5000))
	ts = em.nextFrame()
	em.emit(ts, overlapDisappeared("cand-A"))

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed with only A's history.
	if _, err := AdvanceAccountingV2(ctx, db, "temporal overlap seed prefix", 0); err != nil {
		t.Fatal(err)
	}

	// Logical B appears one logical hour later.
	for i := 0; i < 55; i++ {
		em.nextFrame()
	}
	ts = em.nextFrame()
	em.emit(ts, overlapLogical("logical-B", 5050, 4980))
	ts = em.nextFrame()
	em.emit(ts, overlapDisappeared("logical-B"))

	if _, err := AdvanceAccountingV2(ctx, db, "temporal overlap incremental", 0); err != nil {
		t.Fatal(err)
	}
	checkOverlapOutcome(t, ctx, db)
}

// TestRelayOverlapUsesAuthoritativeBoundaryFrameTime pins the boundary frame
// contract: an active candidate whose last per-connection event predates a
// later logical was still present in the boundary frame (sparse presence
// evidence), so the pair must be evaluated against the authoritative frame
// time and confirm.
func TestRelayOverlapUsesAuthoritativeBoundaryFrameTime(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	s, err := OpenSQLiteSink(ctx, dbPath, overlapSession, "test")
	if err != nil {
		t.Fatal(err)
	}
	em := &overlapEmitter{t: t, s: s, session: overlapSession}

	// Active candidate C observed once at 00:01, then silent (no traffic, no
	// checkpoint due within the test window).
	ts := em.nextFrame()
	em.emit(ts, overlapCandidate("cand-C", 5000, 5000))

	// Logical D appears at 00:05 on the same chain with matching totals.
	for i := 0; i < 3; i++ {
		em.nextFrame()
	}
	ts = em.nextFrame()
	em.emit(ts, overlapLogical("logical-D", 5050, 4980))

	// Sparse frames: only per-frame sampling residuals, no connection events.
	for i := 0; i < 2; i++ {
		ts = em.nextFrame()
		em.emit(ts, &types.CollectorEvent{Type: types.EventSamplingResidual})
	}

	if err := s.EndSession(ctx, overlapSession, SessionStatusClosedClean); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := AdvanceAccountingV2(ctx, db, "boundary frame time", 0); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `
		SELECT status FROM relay_relations_v2 WHERE candidate_connection_id='cand-C';`).Scan(&status); err != nil {
		t.Fatalf("candidate relation missing: %v", err)
	}
	if status != string(RelayConfirmed) {
		t.Fatalf("active candidate present at the boundary frame must pair with the coexisting logical, got %q", status)
	}
}

// TestV2LifecycleMarkerSetAndCleared pins the lifecycle-explicit state
// contract across chunk boundaries and within one chunk.
func TestV2LifecycleMarkerSetAndCleared(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	s, err := OpenSQLiteSink(ctx, dbPath, overlapSession, "test")
	if err != nil {
		t.Fatal(err)
	}
	em := &overlapEmitter{t: t, s: s, session: overlapSession}

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	marker := func(connID string) any {
		t.Helper()
		var d any
		if err := db.QueryRowContext(ctx, `
			SELECT disappeared_at FROM accounting_conn_state_v2 WHERE connection_id = ?;`, connID).Scan(&d); err != nil {
			t.Fatalf("conn state for %s: %v", connID, err)
		}
		return d
	}

	// Seed: conn-lifecycle New at 00:01.
	ts := em.nextFrame()
	em.emit(ts, overlapLogical("conn-lifecycle", 100, 200))
	if _, err := AdvanceAccountingV2(ctx, db, "lifecycle seed", 0); err != nil {
		t.Fatal(err)
	}
	if marker("conn-lifecycle") != nil {
		t.Fatalf("active connection must have no disappeared marker")
	}

	// Chunk 2: Disappeared -> terminal marker set.
	ts = em.nextFrame()
	em.emit(ts, overlapDisappeared("conn-lifecycle"))
	if _, err := AdvanceAccountingV2(ctx, db, "lifecycle disappear", 0); err != nil {
		t.Fatal(err)
	}
	if marker("conn-lifecycle") == nil {
		t.Fatalf("disappeared connection must carry the terminal marker")
	}

	// Chunk 3: legal re-open (New) -> marker explicitly cleared.
	ts = em.nextFrame()
	em.emit(ts, overlapLogical("conn-lifecycle", 50, 70))
	if _, err := AdvanceAccountingV2(ctx, db, "lifecycle reopen", 0); err != nil {
		t.Fatal(err)
	}
	if marker("conn-lifecycle") != nil {
		t.Fatalf("re-opened connection must have the terminal marker cleared")
	}

	// Chunk 4: Disappeared again -> terminal again.
	ts = em.nextFrame()
	em.emit(ts, overlapDisappeared("conn-lifecycle"))
	if _, err := AdvanceAccountingV2(ctx, db, "lifecycle re-disappear", 0); err != nil {
		t.Fatal(err)
	}
	if marker("conn-lifecycle") == nil {
		t.Fatalf("re-disappeared connection must be terminal again")
	}

	// Same-chunk orderings. conn-new-then-gone: New then Disappeared in one
	// chunk must end terminal. conn-gone-then-back (seeded earlier): a chunk
	// containing Disappeared then New must end active.
	ts = em.nextFrame()
	em.emit(ts, overlapLogical("conn-gone-then-back", 1, 2))
	if _, err := AdvanceAccountingV2(ctx, db, "lifecycle pre-seed", 0); err != nil {
		t.Fatal(err)
	}
	ts = em.nextFrame()
	em.emit(ts, overlapLogical("conn-new-then-gone", 10, 20))
	em.emit(ts, overlapDisappeared("conn-new-then-gone"))
	ts = em.nextFrame()
	em.emit(ts, overlapDisappeared("conn-gone-then-back"))
	em.emit(ts, overlapLogical("conn-gone-then-back", 30, 40))
	if _, err := AdvanceAccountingV2(ctx, db, "lifecycle same chunk", 0); err != nil {
		t.Fatal(err)
	}
	if marker("conn-new-then-gone") == nil {
		t.Fatalf("New -> Disappeared within one chunk must end terminal")
	}
	if marker("conn-gone-then-back") != nil {
		t.Fatalf("Disappeared -> New within one chunk must end active")
	}
}
