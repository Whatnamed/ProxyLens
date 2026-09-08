package supervisorapp

import (
	"bytes"
	"context"
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
)

// Streams controls the process-facing I/O used by the shared Supervisor
// daemon entry point. The CLI supplies the real terminal/pipes; the scheduled
// background host supplies discarded output and no stdin because its exact
// local stop event is the lifecycle control channel.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (s Streams) normalized() Streams {
	if s.Stdout == nil {
		s.Stdout = io.Discard
	}
	if s.Stderr == nil {
		s.Stderr = io.Discard
	}
	return s
}

// Run starts the existing Supervisor daemon lifecycle with the supplied
// command-line arguments. It is deliberately shared by the human/IPC CLI and
// the Windows GUI-subsystem Task Scheduler host so ownership and restart
// semantics cannot drift between the two entry points.
func Run(args []string, streams Streams) int {
	streams = streams.normalized()
	defaults := proxylensruntime.DefaultRestartPolicy()
	fs := flag.NewFlagSet("proxylens-supervisor", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
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
		_, _ = fmt.Fprintf(streams.Stdout, "ProxyLens Supervisor v%s (go1.24+, windows/amd64)\n", proxylensruntime.SupervisorVersion)
		return 0
	}

	dbResolution, err := proxylensruntime.ResolveWritableDBPath(*dbPath)
	if err != nil {
		_, _ = fmt.Fprintf(streams.Stderr, "Failed to resolve runtime database path: %v\n", err)
		return 1
	}
	currentExecutable, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(streams.Stderr, "Failed to resolve Supervisor executable path: %v\n", err)
		return 1
	}
	resolvedRuntimeExe, err := proxylensruntime.ResolveRuntimeExecutable(
		*runtimeExe,
		os.Getenv("PROXYLENS_RUNTIME_EXE"),
		currentExecutable,
	)
	if err != nil {
		_, _ = fmt.Fprintf(streams.Stderr, "Failed to resolve Runtime executable: %v\n", err)
		return 1
	}

	controllerOverride := strings.TrimSpace(*controllerURL)
	if controllerOverride != "" {
		if os.Getenv(runtimeconfig.E2EModeEnv) == "1" {
			if err := runtimeconfig.ValidateE2EControllerURL(controllerOverride); err != nil {
				_, _ = fmt.Fprintf(streams.Stderr, "Invalid E2E Controller override: %v\n", err)
				return 1
			}
		} else if err := runtimeconfig.ValidateControllerURL(controllerOverride); err != nil {
			_, _ = fmt.Fprintf(streams.Stderr, "Invalid Controller override: %v\n", err)
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
		Logger:        func(format string, args ...any) { _, _ = fmt.Fprintf(streams.Stderr, format+"\n", args...) },
		OnReady: func(info proxylensruntime.SupervisorReadyInfo) {
			if err := emitSupervisorSignal(streams.Stdout, func(writer io.Writer) error {
				return proxylensruntime.EncodeSupervisorReady(writer, proxylensruntime.SupervisorVersion, os.Getpid(), info)
			}); err != nil {
				_, _ = fmt.Fprintf(streams.Stderr, "Failed to emit Supervisor READY signal: %v\n", err)
			}
		},
		OnRuntimeRestarted: func(info proxylensruntime.SupervisorRuntimeRestartInfo) {
			if err := emitSupervisorSignal(streams.Stdout, func(writer io.Writer) error {
				return proxylensruntime.EncodeSupervisorRuntimeRestarted(writer, proxylensruntime.SupervisorVersion, os.Getpid(), info)
			}); err != nil {
				_, _ = fmt.Fprintf(streams.Stderr, "Failed to emit Runtime restart signal: %v\n", err)
			}
		},
	})
	if err != nil {
		_, _ = fmt.Fprintf(streams.Stderr, "Failed to initialize Supervisor: %v\n", err)
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
	if streams.Stdin != nil {
		go proxylensruntime.ListenForExactStop(streams.Stdin, cancel)
	}

	if err := supervisor.Run(ctx); err != nil {
		if errors.Is(err, proxylensruntime.ErrSupervisorAlreadyRunning) {
			if signalErr := emitSupervisorSignal(streams.Stdout, func(writer io.Writer) error {
				return proxylensruntime.EncodeSupervisorAlreadyRunning(writer, proxylensruntime.SupervisorVersion)
			}); signalErr != nil {
				_, _ = fmt.Fprintf(streams.Stderr, "Failed to emit duplicate Supervisor signal: %v\n", signalErr)
				return 1
			}
			return 0
		}
		_, _ = fmt.Fprintf(streams.Stderr, "[supervisor] fatal exit: %v\n", err)
		return 1
	}
	return 0
}

// emitSupervisorSignal keeps the machine handshake on stdout and, only for an
// explicitly configured E2E status path, appends the same safe PID/status
// record so a lifecycle harness can observe a Supervisor that outlives Tauri.
// The payload is generated by the structured encoders and contains no secret.
func emitSupervisorSignal(stdout io.Writer, encode func(io.Writer) error) error {
	var payload bytes.Buffer
	if err := encode(&payload); err != nil {
		return err
	}
	stdoutErr := error(nil)
	if stdout != nil {
		if _, err := stdout.Write(payload.Bytes()); err != nil {
			stdoutErr = err
		}
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
