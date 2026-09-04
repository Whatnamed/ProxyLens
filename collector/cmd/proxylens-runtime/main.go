package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
	"github.com/Whatnamed/ProxyLens/collector/pkg/runtimeconfig"
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

	e2eMode := os.Getenv(proxylensruntime.E2EModeEnv) == "1"
	persistedConfig, err := loadPersistedRuntimeConfig(e2eMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load runtime config: %v\n", err)
		os.Exit(1)
	}
	secretStore, _, err := runtimeconfig.NewConfiguredSecretStore(e2eMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare secure runtime config: %v\n", err)
		os.Exit(1)
	}
	connection, err := runtimeconfig.ResolveRuntimeConnection(context.Background(), runtimeconfig.RuntimeConnectionOptions{
		CLIControllerURL:         *controllerURL,
		EnvironmentControllerURL: os.Getenv(proxylensruntime.ControllerURLEnv),
		PersistedConfig:          persistedConfig,
		ProductDefaultURL:        defaults.ControllerURL,
		E2EMode:                  e2eMode,
		EnvironmentSecret:        os.Getenv("MIHOMO_SECRET"),
		SecretStore:              secretStore,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve runtime connection: %v\n", err)
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
			ControllerURL:       connection.ControllerURL,
			Secret:              connection.Secret,
			ConnectionsInterval: *connectionsInterval,
			QueueCapacity:       *queueCapacity,
		},
		AccountingInterval: *accountingInterval,
		OnReady: func(info proxylensruntime.RuntimeReadyInfo) {
			_ = proxylensruntime.EncodeRuntimeReady(os.Stdout, info.RuntimeVersion, os.Getpid())
		},
		Logger: log.New(os.Stdout, "", log.LstdFlags).Printf,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize ProxyLens runtime: %v\n", err)
		os.Exit(1)
	}

	metadata := connection.Metadata()
	fmt.Printf("[runtime] database path source=%s controller source=%s secret source=%s secret present=%t\n", resolved.Source, metadata.ControllerSource, metadata.SecretSource, metadata.SecretPresent)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	go proxylensruntime.ListenForExactStop(os.Stdin, cancel)

	result, err := runtime.Run(ctx)
	if err != nil {
		if errors.Is(err, proxylensruntime.ErrRuntimeAlreadyRunning) {
			_ = proxylensruntime.EncodeRuntimeAlreadyRunning(os.Stdout, proxylensruntime.RuntimeVersion)
			return
		}
		fmt.Fprintf(os.Stderr, "[runtime] fatal exit: %v\n", err)
		os.Exit(1)
	}
	if result != nil && result.Collector != nil {
		fmt.Printf("[runtime] session=%s status=%s\n", result.Collector.SessionID, result.Collector.Status)
	}
}

func loadPersistedRuntimeConfig(e2eMode bool) (runtimeconfig.RuntimeConfig, error) {
	if e2eMode {
		return runtimeconfig.DefaultRuntimeConfig(), nil
	}
	path, err := runtimeconfig.ResolveConfigPathFromEnvironment()
	if err != nil {
		// An explicit --db/--controller invocation remains useful on hosts where
		// LOCALAPPDATA is unavailable; there is simply no persisted config yet.
		return runtimeconfig.DefaultRuntimeConfig(), nil
	}
	return runtimeconfig.LoadConfig(path)
}
