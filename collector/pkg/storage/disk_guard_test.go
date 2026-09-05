package storage

import (
	"context"
	"math"
	"testing"
	"time"
)

// The guard trips when measured free space falls below the configured floor
// and stays untripped while the floor is comfortably below reality.
func TestDiskGuardTripSemantics(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/guard-test.db"

	tiny := NewDiskGuard(dbPath)
	tiny.floorFn = func(dbSize uint64) uint64 { return 1 }
	if st := tiny.Check(); st.Tripped {
		t.Fatalf("guard tripped with a 1-byte floor: %+v", st)
	}
	if tiny.Tripped() {
		t.Fatalf("sticky tripped state set by a healthy check")
	}

	huge := NewDiskGuard(dbPath)
	huge.floorFn = func(dbSize uint64) uint64 { return math.MaxUint64 }
	st := huge.Check()
	if !st.Tripped {
		t.Fatalf("guard did not trip with a max floor: %+v", st)
	}
	if !huge.Tripped() {
		t.Fatalf("sticky tripped state not set")
	}
	if st.FreeBytes == 0 {
		t.Fatalf("free space measurement unavailable on this platform: %+v", st)
	}
	// TripLoop must fire exactly once and return without waiting for the
	// context to end.
	trips := 0
	done := make(chan struct{})
	go func() {
		huge.TripLoop(context.Background(), 10*time.Millisecond, func(s DiskGuardStatus) { trips++ })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("TripLoop did not return after tripping")
	}
	if trips != 1 {
		t.Fatalf("onTrip called %d times, want 1", trips)
	}
}

// The immediate first check inside TripLoop trips a guard that starts below
// the floor without waiting a full interval.
func TestDiskGuardTripLoopImmediateTrip(t *testing.T) {
	dir := t.TempDir()
	g := NewDiskGuard(dir + "/guard-immediate.db")
	g.floorFn = func(dbSize uint64) uint64 { return math.MaxUint64 }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	trips := make(chan struct{})
	go g.TripLoop(ctx, 1*time.Hour, func(s DiskGuardStatus) { close(trips) })
	select {
	case <-trips:
	case <-time.After(2 * time.Second):
		t.Fatalf("TripLoop did not fire onTrip for an immediately-breached floor")
	}
}
