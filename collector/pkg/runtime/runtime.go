package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

type LogFunc func(format string, args ...any)

type RuntimeReadyInfo struct {
	RuntimeVersion string
}

type RuntimeReadyCallback func(RuntimeReadyInfo)

type RuntimeOptions struct {
	DBPath             string
	Collector          CollectorOptions
	AccountingInterval time.Duration
	AccountingNotes    string
	Logger             LogFunc
	OnReady            RuntimeReadyCallback
	DiskGuardCheck     func(dbPath string) storage.DiskGuardStatus
}

type Runtime struct {
	dbPath             string
	collector          *CollectorRunner
	accountingInterval time.Duration
	accountingNotes    string
	logger             LogFunc
	onReady            RuntimeReadyCallback
	diskGuardCheck     func(dbPath string) storage.DiskGuardStatus
	readyOnce          sync.Once
}

type RuntimeResult struct {
	DBPath    string
	Collector *CollectorResult
}

type collectorOutcome struct {
	result *CollectorResult
	err    error
}

// NewRuntime prepares the two cooperating components. It does not open the
// database or contact a controller until Run is called.
func NewRuntime(opts RuntimeOptions) (*Runtime, error) {
	if opts.DBPath == "" {
		return nil, fmt.Errorf("runtime requires a resolved database path")
	}

	logger := opts.Logger
	if logger == nil {
		logger = func(string, ...any) {}
	}

	collectorOpts := opts.Collector
	if collectorOpts.DBPath != "" && collectorOpts.DBPath != opts.DBPath {
		return nil, fmt.Errorf("runtime collector DB path %q does not match runtime DB path %q", collectorOpts.DBPath, opts.DBPath)
	}
	collectorOpts.DBPath = opts.DBPath
	if collectorOpts.CollectorVersion == "" {
		collectorOpts.CollectorVersion = RuntimeVersion
	}
	if collectorOpts.Logger == nil {
		collectorOpts.Logger = logger
	}
	priorStatusCallback := collectorOpts.OnControllerStatus
	collectorOpts.OnControllerStatus = func(status string) {
		if priorStatusCallback != nil {
			priorStatusCallback(status)
		}
		logger("[collector] controller status=%s", status)
	}

	collector, err := NewCollectorRunner(collectorOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare collector runner: %w", err)
	}
	interval := opts.AccountingInterval
	if interval <= 0 {
		interval = DefaultAccountingInterval
	}
	notes := opts.AccountingNotes
	if notes == "" {
		notes = "runtime scheduled accounting"
	}

	return &Runtime{
		dbPath:             opts.DBPath,
		collector:          collector,
		accountingInterval: interval,
		accountingNotes:    notes,
		logger:             logger,
		onReady:            opts.OnReady,
		diskGuardCheck:     opts.DiskGuardCheck,
	}, nil
}

func (r *Runtime) DBPath() string {
	return r.dbPath
}

func (r *Runtime) CollectorSessionID() string {
	return r.collector.SessionID()
}

// Run initializes the writer DB, starts collector and scheduler independently,
// and enforces scheduler-first shutdown when the caller cancels the runtime.
func (r *Runtime) Run(ctx context.Context) (runtimeResult *RuntimeResult, runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ownership, err := AcquireRuntimeOwnership(r.dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := ownership.Close(); closeErr != nil && runErr == nil {
			runErr = fmt.Errorf("failed to release runtime ownership: %w", closeErr)
		}
	}()

	// Pre-start capacity check: execute BEFORE opening the writer DB, running
	// WAL pragma, or executing migrations! If the volume is below the stop floor,
	// entering write-quiescent low-disk mode immediately prevents any DB writes.
	var status storage.DiskGuardStatus
	if r.diskGuardCheck != nil {
		status = r.diskGuardCheck(r.dbPath)
	} else {
		status = storage.NewDiskGuard(r.dbPath).Check()
	}
	if status.Tripped {
		r.logger("[runtime] disk guard floor breached before DB init (%s) - low-disk mode: writer DB, migrations, collector, and scheduler disabled; write-quiescent (degraded)",
			status.Describe())
		r.readyOnce.Do(func() {
			if r.onReady != nil {
				r.onReady(RuntimeReadyInfo{RuntimeVersion: RuntimeVersion})
			}
		})
		<-ctx.Done()
		r.logger("[runtime] low-disk mode: graceful shutdown requested")
		return &RuntimeResult{DBPath: r.dbPath}, nil
	}

	db, err := storage.OpenDB(ctx, r.dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize runtime database at %s: %w", r.dbPath, err)
	}

	r.logger("[runtime] version=%s", RuntimeVersion)
	r.logger("[runtime] resolved DB path=%s", r.dbPath)
	cfg := r.collector.Config()
	r.logger("[runtime] controller=%s", cfg.RedactedControllerURL())
	r.logger("[runtime] collector session=%s", r.collector.SessionID())
	r.logger("[runtime] accounting scheduler interval=%s", r.accountingInterval)

	scheduler, err := NewAccountingScheduler(db, r.accountingInterval, r.accountingNotes)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize accounting scheduler: %w", err)
	}
	scheduler.onCheckpoint = func(telemetry AccountingWALTelemetry) {
		if telemetry.Err != nil {
			r.logger("[runtime] wal passive checkpoint failed (health evidence only): %v", telemetry.Err)
			return
		}
		if telemetry.Result.Busy || telemetry.Result.LogFrames != telemetry.Result.CheckpointedFrames {
			r.logger("[runtime] wal checkpoint incomplete busy=%v logFrames=%d checkpointed=%d",
				telemetry.Result.Busy, telemetry.Result.LogFrames, telemetry.Result.CheckpointedFrames)
		}
		if telemetry.WALBytesAvailable {
			r.logger("[runtime] wal telemetry checkpointed=%d/%d walBytes=%d",
				telemetry.Result.CheckpointedFrames, telemetry.Result.LogFrames, telemetry.WALBytes)
		}
	}
	scheduler.onRebuildStart = func(freshness *storage.AccountingFreshness) {
		r.logger("[runtime] accounting catch-up start lagEvents=%d currentSequence=%d", freshness.LagEvents, freshness.CurrentJournalSequenceMax)
	}
	scheduler.onTick = func(result AccountingTickResult) {
		switch result.Action {
		case AccountingSkippedNoEvents:
			r.logger("[runtime] accounting catch-up skipped reason=no_events")
		case AccountingSkippedFresh:
			r.logger("[runtime] accounting catch-up skipped reason=fresh lagEvents=%d", result.Freshness.LagEvents)
		case AccountingRebuilt:
			boundary := int64(0)
			if result.Run.SourceJournalSequenceMax != nil {
				boundary = *result.Run.SourceJournalSequenceMax
			}
			r.logger("[runtime] accounting catch-up completed run=%s boundary=%d", result.Run.RunID, boundary)
		case AccountingFailed:
			r.logger("[runtime] accounting catch-up failed (collector continues): %v", result.Err)
		case AccountingFreshnessFailed:
			r.logger("[runtime] accounting freshness check failed (collector continues): %v", result.Err)
		}
	}

	if err := ctx.Err(); err != nil {
		_ = db.Close()
		return &RuntimeResult{DBPath: r.dbPath}, nil
	}

	diskTripped := false
	var outcome collectorOutcome
	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	defer cancelScheduler()

	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	defer cancelCollector()

	collectorDone := make(chan collectorOutcome, 1)
	go func() {
		result, err := r.collector.Run(collectorCtx)
		collectorDone <- collectorOutcome{result: result, err: err}
	}()

	schedulerDone := make(chan error, 1)
	go func() {
		schedulerDone <- scheduler.Run(schedulerCtx)
	}()

	// The local writer DB, scheduler, and collector goroutine have all crossed
	// their startup boundary. Controller reachability is deliberately not part
	// of this readiness contract because the collector owns retry semantics.
	r.readyOnce.Do(func() {
		if r.onReady != nil {
			r.onReady(RuntimeReadyInfo{RuntimeVersion: RuntimeVersion})
		}
	})

	select {
	case <-ctx.Done():
		r.logger("[runtime] graceful shutdown requested")
		cancelScheduler()
		<-schedulerDone
		cancelCollector()
		outcome = <-collectorDone
	case outcome = <-collectorDone:
		if outcome.err != nil {
			r.logger("[runtime] collector fatal exit: %v", outcome.err)
		} else {
			r.logger("[runtime] collector stopped cleanly")
		}
		cancelScheduler()
		<-schedulerDone
		cancelCollector()
	}

	// Unified check across both select exits: even if runtime ctx cancellation
	// and disk guard trip occur concurrently, any CollectorResult indicating a
	// trip engages write-quiescence and suppresses shutdown accounting/truncate.
	if outcome.result != nil && outcome.result.DiskGuardTripped {
		diskTripped = true
		r.logger("[runtime] collector stopped due to disk guard trip (mid-run breach) - write-quiescent shutdown engaged")
	}
	// Final accounting flush: the collector writer has stopped, so the
	// journal is stable. One chunk is not generally enough for zero lag, so
	// the flush catches up in bounded cycles within a fixed shutdown budget
	// until the generation is fresh (this also drives a still-running seed to
	// completion). If the budget expires the stop stays clean: the remaining
	// lag is explicit health evidence and resumes on next start — the runtime
	// never blocks shutdown indefinitely on accounting.
	if !diskTripped {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _, _ = runShutdownAccountingFlush(
			flushCtx,
			func(ctx context.Context) error {
				_, err := storage.AdvanceAccountingV2(ctx, db, "runtime shutdown flush", 0)
				return err
			},
			func(ctx context.Context) (*storage.AccountingFreshness, error) {
				return storage.NewAnalyticsService(db).GetAccountingFreshness(ctx)
			},
			r.logger,
		)
		flushCancel()

		// Safe maintenance boundary: scheduler and collector writer have both
		// stopped. TRUNCATE runs with a fresh, non-canceled context (the runtime
		// ctx may already be canceled) and an incomplete result is reported as
		// health evidence — a busy Query reader is never killed or blocked.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		cpRes, cpErr := storage.CheckpointWAL(shutdownCtx, db, storage.WALCheckpointTruncateMode)
		shutdownCancel()
		if cpErr != nil {
			r.logger("[runtime] wal shutdown checkpoint failed: %v", cpErr)
		} else if cpRes.Busy || cpRes.LogFrames != cpRes.CheckpointedFrames {
			r.logger("[runtime] wal shutdown checkpoint incomplete busy=%v logFrames=%d checkpointed=%d",
				cpRes.Busy, cpRes.LogFrames, cpRes.CheckpointedFrames)
		} else {
			r.logger("[runtime] wal shutdown checkpoint complete checkpointed=%d", cpRes.CheckpointedFrames)
		}
	} else {
		r.logger("[runtime] low-disk mode: skipped shutdown accounting flush and truncate (write-quiescent)")
	}

	if err := db.Close(); err != nil && outcome.err == nil {
		outcome.err = fmt.Errorf("failed to close runtime database: %w", err)
	}
	r.logger("[runtime] shutdown complete")
	if outcome.err != nil {
		return &RuntimeResult{DBPath: r.dbPath, Collector: outcome.result}, outcome.err
	}
	return &RuntimeResult{DBPath: r.dbPath, Collector: outcome.result}, nil
}

func runShutdownAccountingFlush(
	flushCtx context.Context,
	advance func(context.Context) error,
	getFreshness func(context.Context) (*storage.AccountingFreshness, error),
	logger func(format string, args ...any),
) (cycles int, finalLag int64, err error) {
	flushStart := time.Now()
	finalLag = -1
	for {
		if flushCtx.Err() != nil {
			err = flushCtx.Err()
			break
		}
		if aerr := advance(flushCtx); aerr != nil {
			err = aerr
			break
		}
		cycles++
		fresh, ferr := getFreshness(flushCtx)
		if ferr != nil {
			err = ferr
			break
		}
		finalLag = fresh.LagEvents
		if fresh.LagEvents == 0 || cycles >= 64 {
			// The 64-cycle cap only guards against a pathological no-progress
			// inconsistency between publish and freshness; every cycle is
			// itself bounded by the chunk budget.
			break
		}
	}
	if err != nil {
		if logger != nil {
			logger("[runtime] shutdown accounting flush incomplete after %d cycles in %.1fs (resumes next start): %v",
				cycles, time.Since(flushStart).Seconds(), err)
		}
	} else if finalLag > 0 {
		if logger != nil {
			logger("[runtime] shutdown accounting flush incomplete after %d cycles in %.1fs (lag=%d, resumes next start)",
				cycles, time.Since(flushStart).Seconds(), finalLag)
		}
	} else {
		if logger != nil {
			logger("[runtime] shutdown accounting flush fresh after %d cycles (%.1fs)",
				cycles, time.Since(flushStart).Seconds())
		}
	}
	return cycles, finalLag, err
}
