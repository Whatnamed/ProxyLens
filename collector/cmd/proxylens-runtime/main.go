package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
)

func main() {
	defaults := config.DefaultConfig()
	fs := flag.NewFlagSet("proxylens-runtime", flag.ContinueOnError)
	dbPath := fs.String("db", "", "Optional explicit SQLite database path")
	controllerURL := fs.String("controller", "", "Mihomo external controller URL")
	connectionsInterval := fs.Int("connections-interval", defaults.ConnectionsInterval, "Snapshot interval in milliseconds")
	queueCapacity := fs.Int("queue-capacity", defaults.QueueCapacity, "Bounded collector queue capacity")
	accountingInterval := fs.Duration("accounting-interval", proxylensruntime.DefaultAccountingInterval, "Scheduled accounting interval")
	showVersion := fs.Bool("version", false, "Print runtime version")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *showVersion {
		fmt.Printf("ProxyLens Runtime v%s (go1.24+, windows/amd64)\n", proxylensruntime.RuntimeVersion)
		return
	}

	resolvedControllerURL, controllerSource, err := proxylensruntime.ResolveControllerURLForMode(
		*controllerURL,
		os.Getenv(proxylensruntime.ControllerURLEnv),
		defaults.ControllerURL,
		os.Getenv(proxylensruntime.E2EModeEnv) == "1",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve controller URL: %v\n", err)
		os.Exit(1)
	}

	resolved, err := proxylensruntime.ResolveWritableDBPath(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve runtime database path: %v\n", err)
		os.Exit(1)
	}

	runtime, err := proxylensruntime.NewRuntime(proxylensruntime.RuntimeOptions{
		DBPath: resolved.Path,
		Collector: proxylensruntime.CollectorOptions{
			ControllerURL:       resolvedControllerURL,
			Secret:              defaults.Secret,
			ConnectionsInterval: *connectionsInterval,
			QueueCapacity:       *queueCapacity,
		},
		AccountingInterval: *accountingInterval,
		OnReady: func(info proxylensruntime.RuntimeReadyInfo) {
			_ = emitRuntimeSignal(runtimeReadySignal{
				Type:           runtimeReadySignalType,
				RuntimeVersion: info.RuntimeVersion,
				PID:            os.Getpid(),
			})
		},
		Logger: log.New(os.Stdout, "", log.LstdFlags).Printf,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize ProxyLens runtime: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[runtime] database path source=%s controller source=%s\n", resolved.Source, controllerSource)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := runtime.Run(ctx)
	if err != nil {
		if errors.Is(err, proxylensruntime.ErrRuntimeAlreadyRunning) {
			_ = emitRuntimeSignal(runtimeAlreadyRunningSignal{
				Type:           runtimeAlreadyRunningSignalType,
				RuntimeVersion: proxylensruntime.RuntimeVersion,
			})
			return
		}
		fmt.Fprintf(os.Stderr, "[runtime] fatal exit: %v\n", err)
		os.Exit(1)
	}
	if result != nil && result.Collector != nil {
		fmt.Printf("[runtime] session=%s status=%s\n", result.Collector.SessionID, result.Collector.Status)
	}
}

const (
	runtimeReadySignalType          = "proxylens-runtime-ready"
	runtimeAlreadyRunningSignalType = "proxylens-runtime-already-running"
)

type runtimeReadySignal struct {
	Type           string `json:"type"`
	RuntimeVersion string `json:"runtimeVersion"`
	PID            int    `json:"pid"`
}

type runtimeAlreadyRunningSignal struct {
	Type           string `json:"type"`
	RuntimeVersion string `json:"runtimeVersion"`
}

func emitRuntimeSignal(signal any) error {
	return json.NewEncoder(os.Stdout).Encode(signal)
}
