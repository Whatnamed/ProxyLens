package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

const DefaultAccountingInterval = 30 * time.Second

// AccountingTickAction identifies the scheduler decision for one freshness
// check. Failed rebuilds are intentionally represented as an action rather
// than a Run error so the collector can continue ingesting.
type AccountingTickAction string

const (
	AccountingSkippedNoEvents AccountingTickAction = "skipped_no_events"
	AccountingSkippedFresh    AccountingTickAction = "skipped_fresh"
	AccountingRebuilt         AccountingTickAction = "rebuilt"
	AccountingFailed          AccountingTickAction = "failed"
	AccountingFreshnessFailed AccountingTickAction = "freshness_failed"
	AccountingCancelled       AccountingTickAction = "cancelled"
)

type AccountingFreshnessFunc func(context.Context) (*storage.AccountingFreshness, error)
type AccountingRebuildFunc func(context.Context, string) (*storage.AccountingRunRecord, error)
type AccountingCheckpointFunc func(context.Context) error

// AccountingTickResult is returned by Tick and optionally delivered to the
// scheduler callback after each periodic decision.
type AccountingTickResult struct {
	Action    AccountingTickAction
	Freshness *storage.AccountingFreshness
	Run       *storage.AccountingRunRecord
	Err       error
}

type AccountingSchedulerOptions struct {
	Interval  time.Duration
	Notes     string
	Freshness AccountingFreshnessFunc
	Rebuild   AccountingRebuildFunc
	// Checkpoint bounds WAL growth by truncating the write-ahead log after each
	// scheduled tick. It is best-effort: a checkpoint failure never alters the
	// tick result or stops collection.
	Checkpoint AccountingCheckpointFunc

	// OnRebuildStart is called only after a stale boundary is observed and
	// immediately before invoking Rebuild.
	OnRebuildStart func(*storage.AccountingFreshness)
	OnTick         func(AccountingTickResult)
}

// AccountingScheduler serializes freshness checks and rebuilds. The mutex
// also makes the public Tick method safe for narrow test/integration callers;
// the normal Run loop itself is deliberately single-threaded.
type AccountingScheduler struct {
	interval       time.Duration
	notes          string
	freshness      AccountingFreshnessFunc
	rebuild        AccountingRebuildFunc
	checkpoint     AccountingCheckpointFunc
	onRebuildStart func(*storage.AccountingFreshness)
	onTick         func(AccountingTickResult)
	tickMu         sync.Mutex
}

// NewAccountingScheduler wires the production AnalyticsService and
// RebuildAccounting implementations to a scheduler.
func NewAccountingScheduler(db *sql.DB, interval time.Duration, notes string) (*AccountingScheduler, error) {
	if db == nil {
		return nil, fmt.Errorf("accounting scheduler requires a database")
	}
	analytics := storage.NewAnalyticsService(db)
	return NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Interval: interval,
		Notes:    notes,
		Freshness: func(ctx context.Context) (*storage.AccountingFreshness, error) {
			return analytics.GetAccountingFreshness(ctx)
		},
		Rebuild: func(ctx context.Context, notes string) (*storage.AccountingRunRecord, error) {
			return storage.RebuildAccounting(ctx, db, notes)
		},
		Checkpoint: func(ctx context.Context) error {
			return storage.WALCheckpointTruncate(ctx, db)
		},
	})
}

func NewAccountingSchedulerWithOptions(opts AccountingSchedulerOptions) (*AccountingScheduler, error) {
	if opts.Freshness == nil {
		return nil, fmt.Errorf("accounting scheduler requires a freshness function")
	}
	if opts.Rebuild == nil {
		return nil, fmt.Errorf("accounting scheduler requires a rebuild function")
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultAccountingInterval
	}
	notes := opts.Notes
	if notes == "" {
		notes = "runtime scheduled accounting"
	}
	return &AccountingScheduler{
		interval:       interval,
		notes:          notes,
		freshness:      opts.Freshness,
		rebuild:        opts.Rebuild,
		checkpoint:     opts.Checkpoint,
		onRebuildStart: opts.OnRebuildStart,
		onTick:         opts.OnTick,
	}, nil
}

func (s *AccountingScheduler) Interval() time.Duration {
	return s.interval
}

// Tick performs one serial freshness check and, when necessary, one rebuild.
// Rebuild errors are returned in the result but are not promoted to the Run
// loop as fatal errors.
func (s *AccountingScheduler) Tick(ctx context.Context) AccountingTickResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return AccountingTickResult{Action: AccountingCancelled, Err: err}
	}

	s.tickMu.Lock()
	defer s.tickMu.Unlock()

	if err := ctx.Err(); err != nil {
		return AccountingTickResult{Action: AccountingCancelled, Err: err}
	}
	freshness, err := s.freshness(ctx)
	if err != nil {
		return AccountingTickResult{Action: AccountingFreshnessFailed, Err: err}
	}
	if freshness == nil {
		return AccountingTickResult{
			Action: AccountingFreshnessFailed,
			Err:    fmt.Errorf("freshness function returned nil result"),
		}
	}
	if freshness.LagEvents == 0 {
		if freshness.CurrentJournalSequenceMax == 0 {
			return AccountingTickResult{Action: AccountingSkippedNoEvents, Freshness: freshness}
		}
		return AccountingTickResult{Action: AccountingSkippedFresh, Freshness: freshness}
	}

	if s.onRebuildStart != nil {
		s.onRebuildStart(freshness)
	}
	run, err := s.rebuild(ctx, s.notes)
	if err != nil {
		return AccountingTickResult{Action: AccountingFailed, Freshness: freshness, Err: err}
	}
	if run == nil {
		return AccountingTickResult{
			Action:    AccountingFailed,
			Freshness: freshness,
			Err:       fmt.Errorf("rebuild function returned nil run without an error"),
		}
	}
	return AccountingTickResult{Action: AccountingRebuilt, Freshness: freshness, Run: run}
}

// Run performs an immediate catch-up check and then continues at the
// configured interval until ctx is canceled. Context cancellation is a
// normal scheduler shutdown and returns nil.
func (s *AccountingScheduler) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}

	s.publish(s.Tick(ctx))
	s.runCheckpoint(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.runCheckpoint(ctx)
			return nil
		case <-ticker.C:
			s.publish(s.Tick(ctx))
			s.runCheckpoint(ctx)
		}
	}
}

// runCheckpoint bounds WAL growth after every scheduled tick. Checkpoint
// failures are deliberately silent: the next tick retries, and collection or
// accounting correctness never depends on WAL truncation succeeding.
func (s *AccountingScheduler) runCheckpoint(ctx context.Context) {
	if s.checkpoint == nil {
		return
	}
	_ = s.checkpoint(ctx)
}

func (s *AccountingScheduler) publish(result AccountingTickResult) {
	if s.onTick != nil {
		s.onTick(result)
	}
}
