package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/client"
	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/queue"
	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

const version = "0.4.0-phase2-prototype"

func printUsage() {
	fmt.Println("ProxyLens Collector — Read-Only Network Traffic Auditing Collector")
	fmt.Println("\nUsage:")
	fmt.Println("  collector run [flags]                 Start live read-only collection from Mihomo")
	fmt.Println("  collector storage inspect [flags]     Inspect persisted connections from SQLite database")
	fmt.Println("  collector storage gaps [flags]        List recorded monitoring gaps from SQLite database")
	fmt.Println("  collector storage rebuild [flags]     Rebuild all projections from authoritative event journal")
	fmt.Println("  collector replay <fixture.ndjson>     Deterministic offline replay against test fixture")
	fmt.Println("  collector benchmark <fixture.ndjson>  Run realistic/stress benchmark on snapshot frames")
	fmt.Println("  collector version                     Print collector version")
	fmt.Println("\nFlags for 'run':")
	fmt.Println("  --controller <url>                    Mihomo controller URL (default: http://127.0.0.1:9090)")
	fmt.Println("  --connections-interval <ms>           Snapshot polling cadence in ms (default: 250)")
	fmt.Println("  --db <path>                           SQLite database file path for persistence")
	fmt.Println("  --secret <token>                      Mihomo controller secret (or set MIHOMO_SECRET env)")
	fmt.Println("  --queue-capacity <n>                  Max buffered frames in queue (default: 200)")
	fmt.Println("  --validation-jsonl <path>             Validation event stream output path")
	fmt.Println("  --validation-force-disconnect-after-frames <n> Trigger reconnect gap after N frames")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	subargs := os.Args[2:]

	switch subcmd {
	case "version", "--version", "-v":
		fmt.Printf("ProxyLens Collector v%s (go1.24+, windows/amd64)\n", version)
	case "run":
		runCollector(subargs)
	case "storage":
		runStorageCommand(subargs)
	case "replay":
		runReplay(subargs)
	case "benchmark":
		runBenchmark(subargs)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", subcmd)
		printUsage()
		os.Exit(1)
	}
}

func runCollector(args []string) {
	fs := flag.NewFlagSet("collector run", flag.ContinueOnError)
	controllerURL := fs.String("controller", "http://127.0.0.1:9090", "Mihomo external controller URL")
	interval := fs.Int("connections-interval", 250, "Snapshot interval in ms")
	dbPath := fs.String("db", "", "SQLite database path for durable persistence")
	secret := fs.String("secret", os.Getenv("MIHOMO_SECRET"), "Controller secret")
	queueCapacity := fs.Int("queue-capacity", 200, "Bounded queue capacity")
	validationJSONL := fs.String("validation-jsonl", "", "Optional validation JSONL output path")
	forceDisconnectAfterFrames := fs.Int("validation-force-disconnect-after-frames", 0, "Optional validation fault injection trigger")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	cfg := &config.Config{
		ControllerURL:       *controllerURL,
		ConnectionsInterval: *interval,
		Secret:              *secret,
		QueueCapacity:       *queueCapacity,
		InitialBackoffMs:    500,
		MaxBackoffMs:        10000,
	}

	sessionID := fmt.Sprintf("sess-%d", time.Now().UnixNano())

	fmt.Println("================================================================")
	fmt.Println("ProxyLens Production Collector (Phase 2 Prototype)")
	fmt.Println("================================================================")
	fmt.Printf("Session ID              : %s\n", sessionID)
	fmt.Println(cfg.String())
	if *dbPath != "" {
		fmt.Printf("Storage Mode (SQLite)   : %s (WAL + synchronous=NORMAL)\n", *dbPath)
	} else if *validationJSONL != "" {
		fmt.Printf("Validation JSONL Output : %s\n", *validationJSONL)
	} else {
		fmt.Println("Sink Mode               : Safe ProductionStatsSink (Bounded Memory)")
	}
	fmt.Println("Status: Starting unified read-only ingestion loop...")
	fmt.Println("----------------------------------------------------------------")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Println("\n[SHUTDOWN] Received signal, flushing state engine...")
		case <-ctx.Done():
			return
		}
		cancel()
	}()

	// 监听 Stdin 优雅停止
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			fmt.Println("\n[SHUTDOWN] Received stdin STOP, flushing state engine...")
			cancel()
		}
	}()

	var eventSink sink.EventSink
	var sqliteSink *storage.SQLiteEventSink

	if *dbPath != "" {
		sSink, err := storage.OpenSQLiteSink(ctx, *dbPath, sessionID, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize SQLite storage sink at %s: %v\n", *dbPath, err)
			os.Exit(1)
		}
		sqliteSink = sSink
		eventSink = sSink
	} else if *validationJSONL != "" {
		vSink, err := sink.NewValidationJSONLSink(*validationJSONL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create validation sink: %v\n", err)
			os.Exit(1)
		}
		defer vSink.Close()
		eventSink = vSink
	} else {
		eventSink = sink.NewProductionStatsSink()
	}

	engine := state.NewStateEngine(state.EngineOptions{
		Sink:      eventSink,
		SessionID: sessionID,
	})
	q := queue.NewBoundedQueue[*types.IngestItem](cfg.QueueCapacity)
	c := client.NewControllerClient(cfg)
	c.ValidationForceDisconnectAfterFrames = *forceDisconnectAfterFrames

	var fatalWorkerErr atomic.Value
	workerDone := make(chan struct{})

	// 单一 Worker 协程串行消费有序项
	go func() {
		defer close(workerDone)
		for {
			item, ok := q.Pop(ctx)
			if !ok {
				return
			}
			if err := engine.ProcessIngestItem(item); err != nil {
				fmt.Fprintf(os.Stderr, "[FATAL SINK/STATE ERROR] %v\n", err)
				fatalWorkerErr.Store(err)
				cancel() // 立即通知所有生产者停止
				return
			}
		}
	}()

	// 启动 Controller 监听循环
	err := c.RunStreamLoop(ctx, q)
	if err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Stream loop error: %v\n", err)
		fatalWorkerErr.Store(err)
	}

	q.Close()
	<-workerDone

	if sqliteSink != nil {
		var finalStatus = storage.SessionStatusClosedClean
		if fatalWorkerErr.Load() != nil {
			finalStatus = storage.SessionStatusInterrupted
		}
		if err := sqliteSink.EndSession(context.Background(), sessionID, finalStatus); err != nil {
			fmt.Fprintf(os.Stderr, "[FATAL] Failed to end session: %v\n", err)
			os.Exit(1)
		}
		if err := sqliteSink.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "[FATAL] Failed to close sqlite sink: %v\n", err)
			os.Exit(1)
		}
	}

	if fatalErr := fatalWorkerErr.Load(); fatalErr != nil {
		fmt.Fprintf(os.Stderr, "\n[FATAL EXIT] Collector stopped due to fatal error: %v\n", fatalErr)
		os.Exit(1)
	}

	var summary map[string]any
	if ps, ok := eventSink.(*sink.ProductionStatsSink); ok {
		summary = ps.GetSummary()
	} else if vs, ok := eventSink.(*sink.ValidationJSONLSink); ok {
		summary = vs.GetSummary()
	} else if sqliteSink != nil {
		summary = map[string]any{
			"status":            "Session persisted successfully to SQLite",
			"db":                *dbPath,
			"sessionID":         sessionID,
			"activeConnections": engine.GetActiveConnectionsCount(),
		}
	}

	if summary != nil {
		summary["queueMetrics"] = q.GetMetrics()
		summaryBytes, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Println("----------------------------------------------------------------")
		fmt.Println("Session Summary:")
		fmt.Println(string(summaryBytes))
		fmt.Println("================================================================")
	}
}

func runStorageCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: collector storage <inspect|gaps|rebuild> --db <path> [flags]")
		os.Exit(1)
	}

	action := args[0]
	subargs := args[1:]

	fs := flag.NewFlagSet("collector storage "+action, flag.ContinueOnError)
	dbPath := fs.String("db", "", "Path to SQLite database file")
	latestN := fs.Int("latest", 20, "Number of latest records to display")

	if err := fs.Parse(subargs); err != nil || *dbPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: collector storage %s --db <path>\n", action)
		os.Exit(1)
	}

	ctx := context.Background()
	db, err := storage.OpenDB(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open SQLite database at %s: %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer db.Close()

	qs := storage.NewQueryService(db)

	switch action {
	case "inspect":
		conns, err := qs.ListConnections(ctx, storage.ConnectionFilter{Limit: *latestN})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("================================================================\n")
		fmt.Printf("Persisted Connections in %s (Showing latest %d records):\n", *dbPath, len(conns))
		fmt.Printf("================================================================\n")
		for idx, c := range conns {
			hostOrIP := c.Metadata.Host
			if hostOrIP == "" {
				hostOrIP = c.Metadata.DestinationIP
			}
			fmt.Printf("[%2d] Process: %-16s | Target: %-24s | Route: %-6s | Class: %-18s | Up: %8d B | Down: %8d B | State: %s\n",
				idx+1, c.Metadata.Process, hostOrIP, c.Route, c.LatestAttributionClass,
				c.MonitoredUploadTotal, c.MonitoredDownloadTotal, c.State)
		}

	case "gaps":
		gaps, err := qs.ListMonitoringGaps(ctx, nil, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Query gaps failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("================================================================\n")
		fmt.Printf("Monitoring Gaps in %s (%d records):\n", *dbPath, len(gaps))
		fmt.Printf("================================================================\n")
		for idx, g := range gaps {
			endStr := "ONGOING"
			if g.EndedAt != nil {
				endStr = g.EndedAt.Format(time.RFC3339)
			}
			durStr := "unknown"
			if g.DurationMs != nil {
				durStr = fmt.Sprintf("%d ms", *g.DurationMs)
			}
			fmt.Printf("[%2d] Source: %-26s | Started: %s | Ended: %s | Duration: %s | Reason: %s\n",
				idx+1, g.Source, g.StartedAt.Format(time.RFC3339), endStr, durStr, g.Reason)
		}

	case "rebuild":
		fmt.Printf("Rebuilding all projections from event_journal in %s...\n", *dbPath)
		if err := storage.RebuildProjections(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "Rebuild failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Projections rebuilt successfully!")

	default:
		fmt.Fprintf(os.Stderr, "Unknown storage action: %s\n", action)
		os.Exit(1)
	}
}

func runReplay(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: collector replay <fixture.ndjson>")
		os.Exit(1)
	}
	fixturePath := args[0]

	file, err := os.Open(fixturePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open fixture: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	memSink := sink.NewMemorySink()
	engine := state.NewStateEngine(state.EngineOptions{
		Sink:      memSink,
		SessionID: "replay-canonical-session",
	})

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 64*1024*1024)

	start := time.Now()
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var frame types.ConnectionSnapshotFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			fmt.Fprintf(os.Stderr, "JSON unmarshal failed in replay: %v\n", err)
			os.Exit(1)
		}
		if err := engine.ProcessFrame(&frame); err != nil {
			fmt.Fprintf(os.Stderr, "StateEngine error in replay: %v\n", err)
			os.Exit(1)
		}
	}
	elapsed := time.Since(start)

	summary := memSink.GetSummary()
	summary["fixture"] = filepath.Base(fixturePath)
	summary["wallTimeMs"] = elapsed.Milliseconds()

	summaryBytes, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(summaryBytes))
}

func runBenchmark(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: collector benchmark <fixture.ndjson> [--iterations <n>]")
		os.Exit(1)
	}
	fixturePath := args[0]
	iterations := 1
	for i := 1; i < len(args); i++ {
		if (args[i] == "--iterations" || args[i] == "-iterations") && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &iterations)
		}
	}

	file, err := os.Open(fixturePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open fixture: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	var lines [][]byte
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 64*1024*1024)
	for scanner.Scan() {
		b := scanner.Bytes()
		cpy := make([]byte, len(b))
		copy(cpy, b)
		lines = append(lines, cpy)
	}

	statsSink := sink.NewProductionStatsSink()
	engine := state.NewStateEngine(state.EngineOptions{
		Sink:      statsSink,
		SessionID: "benchmark-session",
	})

	start := time.Now()
	for it := 0; it < iterations; it++ {
		for _, l := range lines {
			var frame types.ConnectionSnapshotFrame
			if err := json.Unmarshal(l, &frame); err != nil {
				fmt.Fprintf(os.Stderr, "JSON unmarshal failed in benchmark: %v\n", err)
				os.Exit(1)
			}
			if err := engine.ProcessFrame(&frame); err != nil {
				fmt.Fprintf(os.Stderr, "StateEngine error in benchmark: %v\n", err)
				os.Exit(1)
			}
		}
	}
	elapsed := time.Since(start)

	summary := statsSink.GetSummary()
	summary["fixture"] = filepath.Base(fixturePath)
	summary["iterations"] = iterations
	summary["totalPushedFrames"] = len(lines) * iterations
	summary["wallTimeMs"] = elapsed.Milliseconds()

	summaryBytes, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(summaryBytes))
}
