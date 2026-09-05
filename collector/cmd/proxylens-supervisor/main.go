package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
	"github.com/Whatnamed/ProxyLens/collector/pkg/runtimeconfig"
	"golang.org/x/term"
)

type configStatus struct {
	SchemaVersion          int    `json:"schemaVersion"`
	ControllerConfigured   bool   `json:"controllerConfigured"`
	ControllerURL          string `json:"controllerUrl,omitempty"`
	PersistedControllerURL string `json:"persistedControllerUrl"`
	EffectiveControllerURL string `json:"effectiveControllerUrl"`
	ControllerSource       string `json:"controllerSource"`
	AutostartEnabled       bool   `json:"autostartEnabled"`
	CredentialStored       bool   `json:"credentialStored"`
	EffectiveSecretPresent bool   `json:"effectiveSecretPresent"`
	SecretPresent          bool   `json:"secretPresent"`
	SecretSource           string `json:"secretSource"`
	InstalledLayout        bool   `json:"installedLayout"`
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "config":
			os.Exit(runConfigCommand(os.Args[2:]))
		case "control":
			os.Exit(runControlCommand(os.Args[2:]))
		case "install":
			os.Exit(runInstallCommand(os.Args[2:]))
		}
	}
	os.Exit(runSupervisor(os.Args[1:]))
}

func runSupervisor(args []string) int {
	defaults := proxylensruntime.DefaultRestartPolicy()
	fs := flag.NewFlagSet("proxylens-supervisor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", "", "Optional explicit SQLite database path")
	runtimeExe := fs.String("runtime-exe", "", "Optional explicit proxylens-runtime executable path")
	controllerURL := fs.String("controller", "", "Optional Controller URL override for the child Runtime")
	probeInterval := fs.Duration("probe-interval", proxylensruntime.DefaultSupervisorProbeInterval, "Runtime presence probe interval")
	readyTimeout := fs.Duration("ready-timeout", proxylensruntime.DefaultSupervisorReadyTimeout, "Runtime readiness timeout")
	stopTimeout := fs.Duration("stop-timeout", proxylensruntime.DefaultSupervisorStopTimeout, "Owned Runtime graceful-stop timeout")
	initialRestartDelay := fs.Duration("restart-initial-delay", defaults.InitialDelay, "Initial Runtime restart delay")
	maxRestartDelay := fs.Duration("restart-max-delay", defaults.MaxDelay, "Maximum Runtime restart delay")
	stableAfter := fs.Duration("restart-stable-after", defaults.StableAfter, "Stable Runtime duration that resets backoff")
	showVersion := fs.Bool("version", false, "Print Supervisor version")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Printf("ProxyLens Supervisor v%s (go1.24+, windows/amd64)\n", proxylensruntime.SupervisorVersion)
		return 0
	}

	dbResolution, err := proxylensruntime.ResolveWritableDBPath(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve runtime database path: %v\n", err)
		return 1
	}
	currentExecutable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve Supervisor executable path: %v\n", err)
		return 1
	}
	resolvedRuntimeExe, err := proxylensruntime.ResolveRuntimeExecutable(
		*runtimeExe,
		os.Getenv("PROXYLENS_RUNTIME_EXE"),
		currentExecutable,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve Runtime executable: %v\n", err)
		return 1
	}

	controllerOverride := strings.TrimSpace(*controllerURL)
	if controllerOverride != "" {
		if os.Getenv(runtimeconfig.E2EModeEnv) == "1" {
			if err := runtimeconfig.ValidateE2EControllerURL(controllerOverride); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid E2E Controller override: %v\n", err)
				return 1
			}
		} else if err := runtimeconfig.ValidateControllerURL(controllerOverride); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid Controller override: %v\n", err)
			return 1
		}
	}

	policy := proxylensruntime.RestartPolicy{
		InitialDelay: *initialRestartDelay,
		MaxDelay:     *maxRestartDelay,
		StableAfter:  *stableAfter,
	}
	if os.Getenv(runtimeconfig.E2EModeEnv) == "1" {
		// E2E keeps the production policy shape but bounds waits for a fast,
		// isolated lifecycle test. The Controller remains supplied by the
		// inherited explicit mock environment.
		if *initialRestartDelay == defaults.InitialDelay {
			policy.InitialDelay = 100 * time.Millisecond
		}
		if *maxRestartDelay == defaults.MaxDelay {
			policy.MaxDelay = 500 * time.Millisecond
		}
		if *stableAfter == defaults.StableAfter {
			policy.StableAfter = time.Second
		}
		if *probeInterval == proxylensruntime.DefaultSupervisorProbeInterval {
			*probeInterval = 100 * time.Millisecond
		}
	}

	supervisor, err := proxylensruntime.NewSupervisor(proxylensruntime.SupervisorOptions{
		DBPath:        dbResolution.Path,
		RuntimeExe:    resolvedRuntimeExe,
		ControllerURL: controllerOverride,
		RestartPolicy: policy,
		ProbeInterval: *probeInterval,
		ReadyTimeout:  *readyTimeout,
		StopTimeout:   *stopTimeout,
		Logger:        func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) },
		OnReady: func(info proxylensruntime.SupervisorReadyInfo) {
			if err := emitSupervisorSignal(func(writer io.Writer) error {
				return proxylensruntime.EncodeSupervisorReady(writer, proxylensruntime.SupervisorVersion, os.Getpid(), info)
			}); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to emit Supervisor READY signal: %v\n", err)
			}
		},
		OnRuntimeRestarted: func(info proxylensruntime.SupervisorRuntimeRestartInfo) {
			if err := emitSupervisorSignal(func(writer io.Writer) error {
				return proxylensruntime.EncodeSupervisorRuntimeRestarted(writer, proxylensruntime.SupervisorVersion, os.Getpid(), info)
			}); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to emit Runtime restart signal: %v\n", err)
			}
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize Supervisor: %v\n", err)
		return 1
	}

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

	if err := supervisor.Run(ctx); err != nil {
		if errors.Is(err, proxylensruntime.ErrSupervisorAlreadyRunning) {
			if signalErr := emitSupervisorSignal(func(writer io.Writer) error {
				return proxylensruntime.EncodeSupervisorAlreadyRunning(writer, proxylensruntime.SupervisorVersion)
			}); signalErr != nil {
				fmt.Fprintf(os.Stderr, "Failed to emit duplicate Supervisor signal: %v\n", signalErr)
				return 1
			}
			return 0
		}
		fmt.Fprintf(os.Stderr, "[supervisor] fatal exit: %v\n", err)
		return 1
	}
	return 0
}

// emitSupervisorSignal keeps the machine handshake on stdout and, only for
// an explicitly configured E2E status path, appends the same safe PID/status
// record so a lifecycle harness can observe a Supervisor that outlives Tauri.
// The payload is generated by the structured encoders and contains no secret.
func emitSupervisorSignal(encode func(io.Writer) error) error {
	var payload bytes.Buffer
	if err := encode(&payload); err != nil {
		return err
	}
	stdoutErr := error(nil)
	if _, err := os.Stdout.Write(payload.Bytes()); err != nil {
		stdoutErr = err
	}
	if os.Getenv(runtimeconfig.E2EModeEnv) == "1" {
		if path := strings.TrimSpace(os.Getenv(runtimeconfig.E2EStatusFileEnv)); path != "" {
			file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return fmt.Errorf("failed to write E2E Supervisor status: %w", err)
			}
			_, writeErr := file.Write(payload.Bytes())
			closeErr := file.Close()
			if writeErr != nil {
				return fmt.Errorf("failed to write E2E Supervisor status: %w", writeErr)
			}
			if closeErr != nil {
				return fmt.Errorf("failed to close E2E Supervisor status: %w", closeErr)
			}
		}
	}
	return stdoutErr
}

func runConfigCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: proxylens-supervisor config <status|apply|set-controller|set-autostart|set-secret|clear-secret>")
		return 2
	}
	switch args[0] {
	case "status":
		return configStatusCommand()
	case "apply":
		return configApplyCommand()
	case "set-controller":
		return configSetControllerCommand(args[1:])
	case "set-autostart":
		return configSetAutostartCommand(args[1:])
	case "set-secret":
		return configSetSecretCommand()
	case "clear-secret":
		return configClearSecretCommand()
	default:
		fmt.Fprintf(os.Stderr, "Unknown config command %q\n", args[0])
		return 2
	}
}

func configStatusCommand() int {
	status, err := buildConfigStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect runtime config: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to emit runtime config status: %v\n", err)
		return 1
	}
	return 0
}

func configSetControllerCommand(args []string) int {
	fs := flag.NewFlagSet("config set-controller", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	controllerURL := fs.String("controller", "", "Controller URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	value := strings.TrimSpace(*controllerURL)
	if os.Getenv(runtimeconfig.E2EModeEnv) == "1" {
		if err := runtimeconfig.ValidateE2EControllerURL(value); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid E2E Controller URL: %v\n", err)
			return 1
		}
	} else if err := runtimeconfig.ValidateControllerURL(value); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid Controller URL: %v\n", err)
		return 1
	}
	cfg, err := loadRuntimeConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load runtime config: %v\n", err)
		return 1
	}
	cfg.ControllerURL = value
	if err := saveRuntimeConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save runtime config: %v\n", err)
		return 1
	}
	return 0
}

func configSetSecretCommand() int {
	store, err := configuredSecretStoreForCommand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare secure runtime config: %v\n", err)
		return 1
	}
	value, err := readControllerSecret(os.Stdin, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if strings.TrimSpace(value) == "" {
		fmt.Fprintln(os.Stderr, "Controller Secret must be non-empty")
		return 1
	}
	if err := store.Write(context.Background(), value); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to persist Controller Secret securely: %v\n", err)
		return 1
	}
	return 0
}

func readControllerSecret(stdin *os.File, prompt io.Writer) (string, error) {
	if stdin == nil {
		return "", fmt.Errorf("Controller Secret stdin is unavailable")
	}
	if term.IsTerminal(int(stdin.Fd())) {
		if prompt != nil {
			_, _ = io.WriteString(prompt, "Controller Secret: ")
		}
		value, err := term.ReadPassword(int(stdin.Fd()))
		if prompt != nil {
			_, _ = io.WriteString(prompt, "\n")
		}
		if err != nil {
			return "", fmt.Errorf("failed to read Controller Secret securely: %w", err)
		}
		return string(value), nil
	}

	// Pipes and lifecycle harnesses retain the existing one-line contract.
	reader := bufio.NewScanner(stdin)
	if !reader.Scan() {
		if err := reader.Err(); err != nil {
			return "", fmt.Errorf("failed to read Controller Secret from stdin")
		}
		return "", fmt.Errorf("Controller Secret must be provided as one non-empty stdin line")
	}
	return reader.Text(), nil
}

func configClearSecretCommand() int {
	store, err := configuredSecretStoreForCommand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare secure runtime config: %v\n", err)
		return 1
	}
	if err := store.Delete(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear Controller Secret securely: %v\n", err)
		return 1
	}
	return 0
}

func loadRuntimeConfig() (runtimeconfig.RuntimeConfig, error) {
	path, err := runtimeconfig.ResolveConfigPathFromEnvironment()
	if err != nil {
		return runtimeconfig.RuntimeConfig{}, err
	}
	return runtimeconfig.LoadConfig(path)
}

func saveRuntimeConfig(cfg runtimeconfig.RuntimeConfig) error {
	path, err := runtimeconfig.ResolveConfigPathFromEnvironment()
	if err != nil {
		return err
	}
	return runtimeconfig.SaveConfig(path, cfg)
}

func configuredSecretStoreForCommand() (runtimeconfig.SecretStore, error) {
	store, _, err := runtimeconfig.NewConfiguredSecretStore(os.Getenv(runtimeconfig.E2EModeEnv) == "1")
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("Windows Credential Manager target is unavailable")
	}
	return store, nil
}
