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
}

type Runtime struct {
	dbPath             string
	collector          *CollectorRunner
	accountingInterval time.Duration
	accountingNotes    string
	logger             LogFunc
	onReady            RuntimeReadyCallback
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

	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	defer cancelCollector()
	defer cancelScheduler()

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

	var outcome collectorOutcome
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

	if err := db.Close(); err != nil && outcome.err == nil {
		outcome.err = fmt.Errorf("failed to close runtime database: %w", err)
	}
	r.logger("[runtime] shutdown complete")
	if outcome.err != nil {
		return &RuntimeResult{DBPath: r.dbPath, Collector: outcome.result}, outcome.err
	}
	return &RuntimeResult{DBPath: r.dbPath, Collector: outcome.result}, nil
}
