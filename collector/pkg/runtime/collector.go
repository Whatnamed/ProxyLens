package runtime

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/client"
	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/queue"
	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

const (
	// DefaultCollectorVersion identifies the collector implementation stored in
	// collector_sessions. Keep the legacy collector CLI version stable while the
	// runtime executable evolves independently.
	DefaultCollectorVersion = "0.6.0-phase2b2-runtime"
	RuntimeVersion          = "0.7.0-phase3e2a"
)

// CollectorOptions contains only collector configuration. CLI parsing,
// process signals, and stdin ownership remain outside the reusable runner.
type CollectorOptions struct {
	ControllerURL       string
	ConnectionsInterval int
	QueueCapacity       int
	Secret              string
	DBPath              string

	ValidationJSONL                      string
	ValidationForceDisconnectAfterFrames int

	InitialBackoffMs int
	MaxBackoffMs     int

	SessionID        string
	CollectorVersion string

	// OnControllerStatus receives redacted lifecycle states only. It must not
	// be used to pass credentials or controller payloads to a logger.
	OnControllerStatus func(status string)
}

// CollectorResult describes the completed runner without exposing any secret
// or controller payload. A context cancellation is a normal closed_clean run.
type CollectorResult struct {
	SessionID         string
	Status            string
	CleanShutdown     bool
	QueueMetrics      queue.Metrics
	ActiveConnections int
	Summary           map[string]any
}

// CollectorRunner owns one collector session and its queue/worker lifecycle.
// It is intentionally single-use: a new session should get a new runner.
type CollectorRunner struct {
	config           *config.Config
	options          CollectorOptions
	sessionID        string
	collectorVersion string
	started          atomic.Bool
}

// NewCollectorRunner validates and prepares a reusable live collector runner.
// ControllerURL must be explicit so reusable callers cannot silently inherit
// the CLI's real-controller default.
// It does not open a database, connect a controller, parse flags, or create
// any process-wide signal handlers.
func NewCollectorRunner(opts CollectorOptions) (*CollectorRunner, error) {
	if strings.TrimSpace(opts.ControllerURL) == "" {
		return nil, fmt.Errorf("collector runner requires an explicit ControllerURL")
	}

	cfg := config.DefaultConfig()
	if opts.ControllerURL != "" {
		cfg.ControllerURL = opts.ControllerURL
	}
	if opts.ConnectionsInterval != 0 {
		cfg.ConnectionsInterval = opts.ConnectionsInterval
	}
	if opts.QueueCapacity != 0 {
		cfg.QueueCapacity = opts.QueueCapacity
	}
	if opts.Secret != "" {
		cfg.Secret = opts.Secret
	}
	if opts.InitialBackoffMs != 0 {
		cfg.InitialBackoffMs = opts.InitialBackoffMs
	}
	if opts.MaxBackoffMs != 0 {
		cfg.MaxBackoffMs = opts.MaxBackoffMs
	}

	if cfg.ConnectionsInterval <= 0 {
		cfg.ConnectionsInterval = 250
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = 200
	}
	if cfg.InitialBackoffMs <= 0 {
		cfg.InitialBackoffMs = 500
	}
	if cfg.MaxBackoffMs <= 0 {
		cfg.MaxBackoffMs = 10000
	}

	u, err := url.Parse(cfg.ControllerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid controller URL: %s", cfg.RedactedControllerURL())
	}

	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	collectorVersion := opts.CollectorVersion
	if collectorVersion == "" {
		collectorVersion = DefaultCollectorVersion
	}

	return &CollectorRunner{
		config:           &cfg,
		options:          opts,
		sessionID:        sessionID,
		collectorVersion: collectorVersion,
	}, nil
}

// SessionID returns the immutable session identity assigned to this runner.
func (r *CollectorRunner) SessionID() string {
	return r.sessionID
}

// Config returns a copy of the normalized collector configuration. The
// secret remains in memory only and is never included in String output.
func (r *CollectorRunner) Config() config.Config {
	return *r.config
}

// Run executes one live collector session. It returns a normal result on
// caller cancellation and an error for fatal stream, sink, or shutdown
// failures. No os.Exit, signal handler, or stdin reader is used here.
func (r *CollectorRunner) Run(ctx context.Context) (*CollectorResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !r.started.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("collector runner for session %s has already run", r.sessionID)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var eventSink sink.EventSink
	var sqliteSink *storage.SQLiteEventSink
	var err error

	switch {
	case r.options.DBPath != "":
		sqliteSink, err = storage.OpenSQLiteSink(runCtx, r.options.DBPath, r.sessionID, r.collectorVersion)
		if err == nil {
			eventSink = sqliteSink
		}
	case r.options.ValidationJSONL != "":
		var validationSink *sink.ValidationJSONLSink
		validationSink, err = sink.NewValidationJSONLSink(r.options.ValidationJSONL)
		if err == nil {
			eventSink = validationSink
		}
	default:
		eventSink = sink.NewProductionStatsSink()
	}
	if err != nil {
		return nil, err
	}

	// Fail-safe capacity boundary: when free space on the DB volume falls
	// below the stop floor the session stops cleanly with explicit journal
	// evidence instead of running into disk-full. The guard never deletes
	// anything; recovery is a later runtime start with more free space.
	var diskGuard *storage.DiskGuard
	if r.options.DBPath != "" && sqliteSink != nil {
		diskGuard = storage.NewDiskGuard(r.options.DBPath)
	}

	engine := state.NewStateEngine(state.EngineOptions{
		Sink:      eventSink,
		SessionID: r.sessionID,
	})
	q := queue.NewBoundedQueue[*types.IngestItem](r.config.QueueCapacity)
	c := client.NewControllerClient(r.config)
	c.ValidationForceDisconnectAfterFrames = r.options.ValidationForceDisconnectAfterFrames
	c.StatusCallback = r.options.OnControllerStatus

	fatalErrCh := make(chan error, 1)
	recordFatal := func(fatalErr error) {
		if fatalErr == nil {
			return
		}
		select {
		case fatalErrCh <- fatalErr:
		default:
		}
	}
	workerDone := make(chan struct{})

	// The guard loop emits its trip evidence through the sink directly (Emit
	// is mutex-guarded), then cancels the private run context: the stream
	// loop returns, the worker drains, and the session ends through the
	// normal clean-shutdown path. The evidence event uses frame 0 / sequence
	// 0, which the engine (sequences >= 1) never produces, so its
	// deterministic event ID cannot collide with a traffic event. The stop
	// decision never depends on the evidence write succeeding.
	if diskGuard != nil {
		go diskGuard.TripLoop(runCtx, storage.DiskGuardInterval, func(status storage.DiskGuardStatus) {
			ev := &types.CollectorEvent{
				SessionID: r.sessionID,
				Timestamp: status.CheckedAt,
				Details: map[string]any{
					"issue":       "disk_guard_floor_breached",
					"description": "free space on the DB volume fell below the stop floor; ingestion stopped cleanly",
					"freeBytes":   int64(status.FreeBytes),
					"floorBytes":  int64(status.FloorBytes),
					"dbSizeBytes": int64(status.DBSizeBytes),
				},
			}
			ev.Type = types.EventCollectorHealth
			ev.GenerateDeterministicEventID()
			_ = sqliteSink.Emit(ev)
			cancel()
		})
	}

	// A single worker preserves frame/event ordering and joins before the
	// session is ended. A sink failure is collector-fatal and cancels the
	// producer through the private run context.
	go func() {
		defer close(workerDone)
		for {
			item, ok := q.Pop(runCtx)
			if !ok {
				return
			}
			if err := engine.ProcessIngestItem(item); err != nil {
				recordFatal(fmt.Errorf("sink/state worker failed: %w", err))
				cancel()
				return
			}
		}
	}()

	streamErr := c.RunStreamLoop(runCtx, q)
	if streamErr != nil && runCtx.Err() == nil {
		recordFatal(fmt.Errorf("controller stream loop failed: %w", streamErr))
		cancel()
	}

	q.Close()
	<-workerDone

	var fatalErr error
	select {
	case fatalErr = <-fatalErrCh:
	default:
	}

	status := storage.SessionStatusClosedClean
	if fatalErr != nil {
		status = storage.SessionStatusInterrupted
	}

	var cleanupErr error
	if sqliteSink != nil {
		// A session that never observed a healthy controller frame must record
		// its whole interval as a controller_stream monitoring gap before the
		// sinks close, so the unobserved window is never counted as covered.
		if err := engine.EmitUnobservedSessionGap(time.Now()); err != nil && cleanupErr == nil {
			cleanupErr = fmt.Errorf("failed to record unobserved session gap: %w", err)
		}
		endCtx, endCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := sqliteSink.EndSession(endCtx, r.sessionID, status); err != nil {
			cleanupErr = fmt.Errorf("failed to end collector session: %w", err)
		}
		endCancel()

		if err := sqliteSink.Close(); err != nil && cleanupErr == nil {
			cleanupErr = fmt.Errorf("failed to close sqlite sink: %w", err)
		}
	} else if eventSink != nil {
		if err := eventSink.Close(); err != nil {
			cleanupErr = fmt.Errorf("failed to close collector sink: %w", err)
		}
	}

	if cleanupErr != nil && fatalErr == nil {
		fatalErr = cleanupErr
		status = storage.SessionStatusInterrupted
	}

	summary := collectorSummary(eventSink, sqliteSink, r.options.DBPath, r.sessionID, engine)
	metrics := q.GetMetrics()
	if summary == nil {
		summary = make(map[string]any)
	}
	summary["queueMetrics"] = metrics
	if diskGuard != nil {
		if st := diskGuard.Status(); !st.CheckedAt.IsZero() {
			summary["diskGuard"] = st.Describe()
			if diskGuard.Tripped() {
				summary["diskGuardTripped"] = true
			}
		}
	}

	result := &CollectorResult{
		SessionID:         r.sessionID,
		Status:            string(status),
		CleanShutdown:     fatalErr == nil,
		QueueMetrics:      metrics,
		ActiveConnections: engine.GetActiveConnectionsCount(),
		Summary:           summary,
	}
	if fatalErr != nil {
		return result, fatalErr
	}
	return result, nil
}

func collectorSummary(eventSink sink.EventSink, sqliteSink *storage.SQLiteEventSink, dbPath, sessionID string, engine *state.StateEngine) map[string]any {
	switch s := eventSink.(type) {
	case *sink.ProductionStatsSink:
		return s.GetSummary()
	case *sink.ValidationJSONLSink:
		return s.GetSummary()
	case *storage.SQLiteEventSink:
		return map[string]any{
			"status":            "Session persisted successfully to SQLite",
			"db":                dbPath,
			"sessionID":         sessionID,
			"activeConnections": engine.GetActiveConnectionsCount(),
		}
	default:
		if sqliteSink != nil {
			return map[string]any{
				"status":            "Session persisted successfully to SQLite",
				"db":                dbPath,
				"sessionID":         sessionID,
				"activeConnections": engine.GetActiveConnectionsCount(),
			}
		}
		return nil
	}
}
