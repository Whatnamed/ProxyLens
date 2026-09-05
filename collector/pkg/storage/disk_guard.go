package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DiskGuardStopFloorBytes is the absolute minimum free space the collector
// requires on its DB volume before ingesting. Measured production growth is
// roughly 175-190 MiB/h, so a 1 GiB floor leaves hours of margin before the
// volume could actually fill, and keeps WAL headroom even when the DB file is
// small.
const DiskGuardStopFloorBytes = 1 << 30

// DiskGuardDBSizeRatioFloor additionally requires 15% of the current DB file
// size as free space, mirroring the v2 seed preflight.
const DiskGuardDBSizeRatioFloor = 15

// DiskGuardInterval is how often the collector re-measures free space while
// a session runs.
const DiskGuardInterval = 30 * time.Second

// DiskGuardStatus is one free-space measurement against the stop floor.
type DiskGuardStatus struct {
	FreeBytes   uint64
	FloorBytes  uint64
	DBSizeBytes uint64
	Tripped     bool
	CheckedAt   time.Time
}

// Describe renders the status for logs and session summaries without any
// sensitive information (byte counts only).
func (s DiskGuardStatus) Describe() string {
	return fmt.Sprintf("free=%dMiB floor=%dMiB db=%dMiB", s.FreeBytes>>20, s.FloorBytes>>20, s.DBSizeBytes>>20)
}

// DiskGuard watches free space on the collector's DB volume and trips when it
// falls below the stop floor. Tripping is a fail-safe capacity boundary: the
// collector stops ingesting cleanly and records explicit evidence instead of
// running the volume into disk-full. The guard never deletes or compacts
// anything — raw authority retention is never its decision.
//
// Tripped is sticky for the lifetime of the guard: a session that has seen
// the floor breached stops and does not resume mid-session. A later runtime
// start uses a fresh guard and may begin again once space has recovered.
type DiskGuard struct {
	dbPath  string
	floorFn func(dbSize uint64) uint64

	mu      sync.Mutex
	last    DiskGuardStatus
	tripped bool
}

// NewDiskGuard watches the volume holding dbPath. The stop floor is
// max(DiskGuardStopFloorBytes, DiskGuardDBSizeRatioFloor% of the DB file
// size), re-evaluated from the current DB size on every check. An
// unmeasurable state (missing dir, unknown free space) never trips the
// guard: fail-safe behavior must be driven by evidence, not by a broken
// probe.
func NewDiskGuard(dbPath string) *DiskGuard {
	return &DiskGuard{
		dbPath: dbPath,
		floorFn: func(dbSize uint64) uint64 {
			floor := uint64(DiskGuardStopFloorBytes)
			if ratioFloor := dbSize / 100 * DiskGuardDBSizeRatioFloor; ratioFloor > floor {
				floor = ratioFloor
			}
			return floor
		},
	}
}

// Check measures free space once and updates the tripped state.
func (g *DiskGuard) Check() DiskGuardStatus {
	var dbSize uint64
	if fi, err := os.Stat(g.dbPath); err == nil && fi.Size() > 0 {
		dbSize = uint64(fi.Size())
	}
	free, ok := freeDiskBytes(filepath.Dir(g.dbPath))
	status := DiskGuardStatus{
		FreeBytes:   free,
		FloorBytes:  g.floorFn(dbSize),
		DBSizeBytes: dbSize,
		CheckedAt:   time.Now().UTC(),
	}
	if ok && free < status.FloorBytes {
		status.Tripped = true
	}

	g.mu.Lock()
	g.last = status
	if status.Tripped {
		g.tripped = true
	}
	g.mu.Unlock()
	return status
}

// Status returns the most recent measurement. Zero value means no check ran
// yet.
func (g *DiskGuard) Status() DiskGuardStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.last
}

// Tripped reports whether any measurement has fallen below the floor.
func (g *DiskGuard) Tripped() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tripped
}

// TripLoop re-checks at the given interval until ctx is canceled. onTrip is
// called exactly once with the tripped status — either immediately when the
// first check is already below the floor, or on the first healthy->tripped
// transition later in the session.
func (g *DiskGuard) TripLoop(ctx context.Context, interval time.Duration, onTrip func(DiskGuardStatus)) {
	if g.Check().Tripped && onTrip != nil {
		onTrip(g.Status())
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if g.Check().Tripped && onTrip != nil {
				onTrip(g.Status())
				return
			}
		}
	}
}
