package main

import (
	"bufio"
	"context"
	"database/sql"
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

const version = "0.6.0-phase2b2-runtime"

func printUsage() {
	fmt.Println("ProxyLens Collector — Read-Only Network Traffic Auditing Collector")
	fmt.Println("\nUsage:")
	fmt.Println("  collector run [flags]                 Start live read-only collection from Mihomo")
	fmt.Println("  collector storage inspect [flags]     Inspect persisted connections from SQLite database")
	fmt.Println("  collector storage gaps [flags]        List recorded monitoring gaps from SQLite database")
	fmt.Println("  collector storage rebuild [flags]     Rebuild all projections from authoritative event journal")
	fmt.Println("  collector storage cleanup [flags]     Dry-run or apply safe derived data retention cleanup")
	fmt.Println("  collector storage integrity [flags]   Run SQLite integrity and foreign key validation")
	fmt.Println("  collector accounting rebuild [flags]  Reconcile relay deductions and rebuild accounted traffic")
	fmt.Println("  collector analytics summary [flags]   Show reconciled usage & coverage summary")
	fmt.Println("  collector analytics top-processes [flags] Show top process traffic breakdown")
	fmt.Println("  collector analytics top-hosts [flags] Show top destination host breakdown")
	fmt.Println("  collector analytics top-proxies [flags] Show top outbound proxy node breakdown")
	fmt.Println("  collector analytics coverage [flags]  Show time window monitoring coverage analysis")
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
	case "accounting":
		runAccountingCommand(subargs)
	case "analytics":
		runAnalyticsCommand(subargs)
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
	fmt.Println("ProxyLens Production Collector (Phase 2B2 Runtime Validation & Storage Operations)")
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
		fmt.Println("Usage: collector storage <inspect|gaps|rebuild|cleanup|integrity> --db <path> [flags]")
		os.Exit(1)
	}

	action := args[0]
	subargs := args[1:]

	switch action {
	case "inspect":
		fs := flag.NewFlagSet("collector storage inspect", flag.ContinueOnError)
		dbPath := fs.String("db", "", "Path to SQLite database file")
		latestN := fs.Int("latest", 20, "Number of latest records to display")
		jsonOutput := fs.Bool("json", false, "Output in JSON format including journal continuity stats")
		if err := fs.Parse(subargs); err != nil || *dbPath == "" {
			fmt.Fprintf(os.Stderr, "Usage: collector storage inspect --db <path> [--latest <n>] [--json]\n")
			os.Exit(1)
		}

		ctx := context.Background()
		db, err := storage.OpenDB(ctx, *dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open SQLite database at %s: %v\n", *dbPath, err)
			os.Exit(1)
		}
		defer db.Close()

		var journalCount, distinctSeqCount int64
		var minSeq, maxSeq sql.NullInt64
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*), COUNT(DISTINCT journal_sequence), MIN(journal_sequence), MAX(journal_sequence) FROM event_journal;").Scan(
			&journalCount, &distinctSeqCount, &minSeq, &maxSeq,
		)

		isContinuous := false
		if journalCount > 0 && minSeq.Valid && maxSeq.Valid {
			if maxSeq.Int64-minSeq.Int64+1 == journalCount && journalCount == distinctSeqCount {
				isContinuous = true
			}
		}

		qs := storage.NewQueryService(db)
		conns, err := qs.ListConnections(ctx, storage.ConnectionFilter{Limit: *latestN})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
			os.Exit(1)
		}

		if *jsonOutput {
			res := map[string]interface{}{
				"dbPath":           *dbPath,
				"connectionsCount": len(conns),
				"journalStats": map[string]interface{}{
					"count":             journalCount,
					"distinctSequences": distinctSeqCount,
					"minSequence":       minSeq.Int64,
					"maxSequence":       maxSeq.Int64,
					"isContinuous":      isContinuous,
				},
				"connections": conns,
			}
			b, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(b))
			return
		}

		fmt.Printf("================================================================\n")
		fmt.Printf("Persisted Connections in %s (Showing latest %d records):\n", *dbPath, len(conns))
		fmt.Printf("Journal Events: %d (Distinct Seq: %d, Min: %d, Max: %d, Continuous: %v)\n",
			journalCount, distinctSeqCount, minSeq.Int64, maxSeq.Int64, isContinuous)
		fmt.Printf("================================================================\n")
		for idx, c := range conns {
			hostOrIP := c.Metadata.Host
			if hostOrIP == "" {
				hostOrIP = c.Metadata.DestinationIP
			}
			obsStatus := "ACTIVE"
			if !c.ObservationActive {
				obsStatus = fmt.Sprintf("ENDED(%s)", c.ObservationEndReason)
			}
			fmt.Printf("[%2d] Process: %-16s | Target: %-24s | Route: %-6s | Class: %-18s | Up: %8d B | Down: %8d B | Obs: %s\n",
				idx+1, c.Metadata.Process, hostOrIP, c.Route, c.LatestAttributionClass,
				c.MonitoredUploadTotal, c.MonitoredDownloadTotal, obsStatus)
		}

	case "gaps":
		fs := flag.NewFlagSet("collector storage gaps", flag.ContinueOnError)
		dbPath := fs.String("db", "", "Path to SQLite database file")
		if err := fs.Parse(subargs); err != nil || *dbPath == "" {
			fmt.Fprintf(os.Stderr, "Usage: collector storage gaps --db <path>\n")
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
		fs := flag.NewFlagSet("collector storage rebuild", flag.ContinueOnError)
		dbPath := fs.String("db", "", "Path to SQLite database file")
		if err := fs.Parse(subargs); err != nil || *dbPath == "" {
			fmt.Fprintf(os.Stderr, "Usage: collector storage rebuild --db <path>\n")
			os.Exit(1)
		}

		ctx := context.Background()
		db, err := storage.OpenDB(ctx, *dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open SQLite database at %s: %v\n", *dbPath, err)
			os.Exit(1)
		}
		defer db.Close()

		fmt.Printf("Rebuilding storage projections from authoritative event journal in %s...\n", *dbPath)
		if err := storage.RebuildProjections(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "[FATAL REBUILD ERROR] %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Storage projections successfully rebuilt!\n")

	case "cleanup":
		fs := flag.NewFlagSet("collector storage cleanup", flag.ContinueOnError)
		dbPath := fs.String("db", "", "Path to SQLite database file")
		applyFlag := fs.Bool("apply", false, "Apply actual deletion of derived data (default: dry-run)")
		dryRunFlag := fs.Bool("dry-run", false, "Explicitly perform dry-run only")
		keepRuns := fs.Int("keep-runs", 3, "Number of latest completed accounting runs to retain")
		retentionDays := fs.Int("retention-days", 7, "Retention window in days for failed runs")

		if err := fs.Parse(subargs); err != nil || *dbPath == "" {
			fmt.Fprintf(os.Stderr, "Usage: collector storage cleanup --db <path> [--keep-runs <n>] [--retention-days <n>] [--apply] [--dry-run]\n")
			os.Exit(1)
		}

		ctx := context.Background()
		db, err := storage.OpenDB(ctx, *dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open SQLite database at %s: %v\n", *dbPath, err)
			os.Exit(1)
		}
		defer db.Close()

		failedAge := time.Duration(*retentionDays) * 24 * time.Hour
		plan, err := storage.PlanDerivedRetention(ctx, db, *keepRuns, failedAge, *dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to compute retention plan: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("================================================================\n")
		fmt.Printf("Safe Derived Data Retention Plan for %s\n", *dbPath)
		fmt.Printf("================================================================\n")
		fmt.Printf("Retain Latest Completed Runs: %d\n", plan.RetainCompletedRuns)
		fmt.Printf("Runs Marked for Deletion    : %d %v\n", len(plan.RunsToDelete), plan.RunsToDelete)
		fmt.Printf("Estimated Derived Rows      : Traffic=%d, Aggregates=%d, RelayRelations=%d\n",
			plan.EstimatedTrafficRows, plan.EstimatedAggregateRows, plan.EstimatedRelayRows)
		fmt.Printf("Current Storage Size        : DB=%d bytes (%.2f MB), WAL=%d bytes (%.2f MB)\n",
			plan.DBSizeBytes, float64(plan.DBSizeBytes)/(1024*1024),
			plan.WALSizeBytes, float64(plan.WALSizeBytes)/(1024*1024))
		fmt.Printf("Raw Authority Invariant     : event_journal & connection_traffic NEVER deleted\n")
		fmt.Printf("----------------------------------------------------------------\n")

		shouldApply := *applyFlag && !*dryRunFlag
		if !shouldApply {
			fmt.Println("[DRY-RUN] No changes were applied. Specify --apply to execute cleanup.")
		} else {
			fmt.Println("[APPLYING] Row-batched deleting derived rows for marked runs...")
			res, err := storage.ApplyDerivedRetention(ctx, db, plan)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ERROR] Retention cleanup failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Cleanup Completed Successfully!\n")
			fmt.Printf("Deleted Runs: %d, Traffic Rows: %d, Aggregate Rows: %d, Relay Rows: %d\n",
				res.DeletedRuns, res.DeletedTrafficRows, res.DeletedAggregateRows, res.DeletedRelayRows)
		}
		fmt.Printf("================================================================\n")

	case "integrity":
		fs := flag.NewFlagSet("collector storage integrity", flag.ContinueOnError)
		dbPath := fs.String("db", "", "Path to SQLite database file")
		if err := fs.Parse(subargs); err != nil || *dbPath == "" {
			fmt.Fprintf(os.Stderr, "Usage: collector storage integrity --db <path>\n")
			os.Exit(1)
		}

		ctx := context.Background()
		db, err := storage.OpenDB(ctx, *dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open SQLite database at %s: %v\n", *dbPath, err)
			os.Exit(1)
		}
		defer db.Close()

		var integCheck string
		if err := db.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integCheck); err != nil {
			fmt.Fprintf(os.Stderr, "PRAGMA integrity_check error: %v\n", err)
			os.Exit(1)
		}
		fkRows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check;")
		if err != nil {
			fmt.Fprintf(os.Stderr, "PRAGMA foreign_key_check error: %v\n", err)
			os.Exit(1)
		}
		defer fkRows.Close()

		var fkViolations int
		for fkRows.Next() {
			fkViolations++
		}
		if err := fkRows.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "PRAGMA foreign_key_check iteration error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("================================================================\n")
		fmt.Printf("SQLite Integrity & Operational Health Report for %s\n", *dbPath)
		fmt.Printf("================================================================\n")
		fmt.Printf("PRAGMA integrity_check   : %s\n", integCheck)
		fmt.Printf("PRAGMA foreign_key_check : %d violations\n", fkViolations)
		if integCheck == "ok" && fkViolations == 0 {
			fmt.Println("Health Status            : HEALTHY (Zero corruption / Zero foreign key violations)")
		} else {
			fmt.Println("Health Status            : DEGRADED / CORRUPTED")
		}
		fmt.Printf("================================================================\n")

	default:
		fmt.Fprintf(os.Stderr, "Unknown storage action: %s\n", action)
		fmt.Println("Usage: collector storage <inspect|gaps|rebuild|cleanup|integrity> --db <path> [flags]")
		os.Exit(1)
	}
}

func runAccountingCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: collector accounting rebuild --db <path> [--notes <str>]")
		os.Exit(1)
	}

	action := args[0]
	subargs := args[1:]

	fs := flag.NewFlagSet("collector accounting "+action, flag.ContinueOnError)
	dbPath := fs.String("db", "", "Path to SQLite database file")
	notes := fs.String("notes", "manual cli rebuild", "Notes for accounting run")

	if err := fs.Parse(subargs); err != nil || *dbPath == "" || action != "rebuild" {
		fmt.Fprintf(os.Stderr, "Usage: collector accounting rebuild --db <path> [--notes <str>]\n")
		os.Exit(1)
	}

	ctx := context.Background()
	db, err := storage.OpenDB(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open SQLite database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Printf("Running versioned reconciled accounting & hourly aggregations on %s...\n", *dbPath)
	run, err := storage.RebuildAccounting(ctx, db, *notes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL ACCOUNTING ERROR] %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Accounting completed successfully!\n")
	fmt.Printf("Run ID                  : %s\n", run.RunID)
	fmt.Printf("Algorithm Version       : %s\n", run.AlgorithmVersion)
	fmt.Printf("Source Events Processed : %d\n", run.SourceJournalEventCount)
	fmt.Printf("Status                  : %s\n", run.Status)
}

func runAnalyticsCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: collector analytics <summary|top-processes|top-hosts|top-proxies|coverage> --db <path> [flags]")
		os.Exit(1)
	}

	action := args[0]
	subargs := args[1:]

	fs := flag.NewFlagSet("collector analytics "+action, flag.ContinueOnError)
	dbPath := fs.String("db", "", "Path to SQLite database file")
	fromStr := fs.String("from", "", "Optional RFC3339 start time")
	toStr := fs.String("to", "", "Optional RFC3339 end time")
	routeFilter := fs.String("route", "", "Optional route filter (PROXY|DIRECT|REJECT)")
	limit := fs.Int("limit", 10, "Top N limit")

	if err := fs.Parse(subargs); err != nil || *dbPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: collector analytics %s --db <path> [flags]\n", action)
		os.Exit(1)
	}

	ctx := context.Background()
	db, err := storage.OpenDB(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open SQLite database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	svc := storage.NewAnalyticsService(db)

	var filter storage.AnalyticsFilter
	if *fromStr != "" {
		t, err := time.Parse(time.RFC3339, *fromStr)
		if err == nil { filter.StartTime = &t }
	}
	if *toStr != "" {
		t, err := time.Parse(time.RFC3339, *toStr)
		if err == nil { filter.EndTime = &t }
	}
	filter.Route = types.RouteType(*routeFilter)
	filter.Limit = *limit

	switch action {
	case "summary":
		summary, err := svc.GetUsageSummary(ctx, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get usage summary: %v\n", err)
			os.Exit(1)
		}
		summaryJSON, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Println(string(summaryJSON))

	case "top-processes":
		items, err := svc.GetTopProcesses(ctx, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
			os.Exit(1)
		}
		printTopItems("Top Processes", items)

	case "top-hosts":
		items, err := svc.GetTopHosts(ctx, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
			os.Exit(1)
		}
		printTopItems("Top Destination Hosts", items)

	case "top-proxies":
		items, err := svc.GetTopFinalProxies(ctx, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
			os.Exit(1)
		}
		printTopItems("Top Outbound Proxies", items)

	case "coverage":
		cov, err := svc.GetCoverage(ctx, filter.StartTime, filter.EndTime)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Coverage query failed: %v\n", err)
			os.Exit(1)
		}
		covJSON, _ := json.MarshalIndent(cov, "", "  ")
		fmt.Println(string(covJSON))

	default:
		fmt.Fprintf(os.Stderr, "Unknown analytics action: %s\n", action)
		os.Exit(1)
	}
}

func printTopItems(title string, items []storage.TopDimensionItem) {
	fmt.Println("================================================================")
	fmt.Printf("%s (%d entries):\n", title, len(items))
	fmt.Println("================================================================")
	for i, item := range items {
		fmt.Printf("[%2d] Key: %-28s | Route: %-6s | Total: %8d B (Up: %6d, Down: %6d) | Conns: %3d\n",
			i+1, item.Key, item.Route, item.TotalBytes, item.UploadBytes, item.DownloadBytes, item.ConnectionCount)
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
