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
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func printUsage() {
	fmt.Println("ProxyLens Collector (Phase 1 Production Prototype)")
	fmt.Println("\nUsage:")
	fmt.Println("  collector run [flags]")
	fmt.Println("  collector replay <fixture.ndjson>")
	fmt.Println("  collector benchmark <fixture.ndjson> [--iterations <n>]")
	fmt.Println("\nFlags:")
	fmt.Println("  --controller <url>           Mihomo External Controller URL (default: http://127.0.0.1:9090)")
	fmt.Println("  --connections-interval <ms>  Snapshot interval in ms (250, 500, 1000, default: 250)")
	fmt.Println("  --secret <secret>            Controller Secret (prefers MIHOMO_SECRET env var)")
	fmt.Println("  --validation-jsonl <path>    Optional path to emit validation JSONL events (for shadow verification)")
	fmt.Println("  --validation-force-disconnect-after-frames <N> Optional fault injection frame trigger")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	subargs := os.Args[2:]

	switch subcmd {
	case "run":
		runCollector(subargs)
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

	fmt.Println("================================================================")
	fmt.Println("ProxyLens Production Collector (Phase 1 Prototype)")
	fmt.Println("================================================================")
	fmt.Println(cfg.String())
	if *validationJSONL != "" {
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

	// 监听 Stdin 优雅停止 (跨平台子进程通信)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			fmt.Println("\n[SHUTDOWN] Received stdin STOP, flushing state engine...")
			cancel()
		}
	}()

	var eventSink sink.EventSink
	if *validationJSONL != "" {
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

	engine := state.NewStateEngine(state.EngineOptions{Sink: eventSink})
	q := queue.NewBoundedQueue[*types.IngestItem](cfg.QueueCapacity)
	c := client.NewControllerClient(cfg)
	c.ValidationForceDisconnectAfterFrames = *forceDisconnectAfterFrames

	var fatalWorkerErr atomic.Value
	workerDone := make(chan struct{})

	// 单 Worker 串行消费统一有序队列 (带 Fail-Stop 机制)
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
	}

	q.Close()
	<-workerDone

	if fatalErr := fatalWorkerErr.Load(); fatalErr != nil {
		fmt.Fprintf(os.Stderr, "\n[FATAL EXIT] Collector stopped due to engine error: %v\n", fatalErr)
		os.Exit(1)
	}

	var summary map[string]any
	if ps, ok := eventSink.(*sink.ProductionStatsSink); ok {
		summary = ps.GetSummary()
	} else if vs, ok := eventSink.(*sink.ValidationJSONLSink); ok {
		summary = vs.GetSummary()
	}

	summary["queueMetrics"] = q.GetMetrics()
	summaryBytes, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println("----------------------------------------------------------------")
	fmt.Println("Session Summary:")
	fmt.Println(string(summaryBytes))
	fmt.Println("================================================================")
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
			continue
		}
		_ = engine.ProcessFrame(&frame)
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
				continue
			}
			_ = engine.ProcessFrame(&frame)
		}
	}
	elapsed := time.Since(start)

	summary := statsSink.GetSummary()
	summary["iterations"] = iterations
	summary["wallTimeMs"] = elapsed.Milliseconds()
	totalFrames := summary["totalFrames"].(int64)
	totalObs := summary["totalObservations"].(int64)
	summary["framesPerSec"] = float64(totalFrames) / elapsed.Seconds()
	summary["observationsPerSec"] = float64(totalObs) / elapsed.Seconds()

	summaryBytes, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(summaryBytes))
}
