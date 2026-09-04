package runtime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	SupervisorVersion              = "0.7.0-phase3e2b1"
	DefaultSupervisorProbeInterval = 2 * time.Second
	DefaultSupervisorReadyTimeout  = 5 * time.Second
	DefaultSupervisorStopTimeout   = 5 * time.Second
	DefaultRestartInitialDelay     = 1 * time.Second
	DefaultRestartMaxDelay         = 30 * time.Second
	DefaultRestartStableAfter      = 60 * time.Second
)

type RestartPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	StableAfter  time.Duration
}

func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		InitialDelay: DefaultRestartInitialDelay,
		MaxDelay:     DefaultRestartMaxDelay,
		StableAfter:  DefaultRestartStableAfter,
	}
}

type RestartBackoff struct {
	policy RestartPolicy
	next   time.Duration
}

func NewRestartBackoff(policy RestartPolicy) *RestartBackoff {
	policy = normalizeRestartPolicy(policy)
	return &RestartBackoff{policy: policy, next: policy.InitialDelay}
}

func (b *RestartBackoff) NextDelay() time.Duration {
	if b == nil {
		return 0
	}
	delay := b.next
	if delay <= 0 {
		delay = b.policy.InitialDelay
	}
	next := delay * 2
	if next < delay || next > b.policy.MaxDelay {
		next = b.policy.MaxDelay
	}
	b.next = next
	return delay
}

func (b *RestartBackoff) Reset() {
	if b == nil {
		return
	}
	b.next = b.policy.InitialDelay
}

func normalizeRestartPolicy(policy RestartPolicy) RestartPolicy {
	defaults := DefaultRestartPolicy()
	if policy.InitialDelay <= 0 {
		policy.InitialDelay = defaults.InitialDelay
	}
	if policy.MaxDelay < policy.InitialDelay {
		policy.MaxDelay = defaults.MaxDelay
		if policy.MaxDelay < policy.InitialDelay {
			policy.MaxDelay = policy.InitialDelay
		}
	}
	if policy.StableAfter <= 0 {
		policy.StableAfter = defaults.StableAfter
	}
	return policy
}

type SupervisorOptions struct {
	DBPath             string
	RuntimeExe         string
	ControllerURL      string
	RestartPolicy      RestartPolicy
	ProbeInterval      time.Duration
	ReadyTimeout       time.Duration
	StopTimeout        time.Duration
	Logger             LogFunc
	OnReady            func(SupervisorReadyInfo)
	OnRuntimeRestarted func(SupervisorRuntimeRestartInfo)
}

type Supervisor struct {
	dbPath             string
	runtimeExe         string
	controllerURL      string
	restartPolicy      RestartPolicy
	probeInterval      time.Duration
	readyTimeout       time.Duration
	stopTimeout        time.Duration
	logger             LogFunc
	onReady            func(SupervisorReadyInfo)
	onRuntimeRestarted func(SupervisorRuntimeRestartInfo)
}

func NewSupervisor(opts SupervisorOptions) (*Supervisor, error) {
	if strings.TrimSpace(opts.DBPath) == "" {
		return nil, fmt.Errorf("supervisor requires a resolved database path")
	}
	if strings.TrimSpace(opts.RuntimeExe) == "" {
		return nil, fmt.Errorf("supervisor requires a resolved Runtime executable")
	}
	if !isRegularFile(opts.RuntimeExe) {
		return nil, fmt.Errorf("supervisor Runtime executable is not a regular file")
	}
	logger := opts.Logger
	if logger == nil {
		logger = func(string, ...any) {}
	}
	probeInterval := opts.ProbeInterval
	if probeInterval <= 0 {
		probeInterval = DefaultSupervisorProbeInterval
	}
	readyTimeout := opts.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = DefaultSupervisorReadyTimeout
	}
	stopTimeout := opts.StopTimeout
	if stopTimeout <= 0 {
		stopTimeout = DefaultSupervisorStopTimeout
	}
	return &Supervisor{
		dbPath:             filepath.Clean(opts.DBPath),
		runtimeExe:         filepath.Clean(opts.RuntimeExe),
		controllerURL:      strings.TrimSpace(opts.ControllerURL),
		restartPolicy:      normalizeRestartPolicy(opts.RestartPolicy),
		probeInterval:      probeInterval,
		readyTimeout:       readyTimeout,
		stopTimeout:        stopTimeout,
		logger:             logger,
		onReady:            opts.OnReady,
		onRuntimeRestarted: opts.OnRuntimeRestarted,
	}, nil
}

// Run owns only the Supervisor mutex. It never opens SQLite or instantiates a
// Controller client; all business work remains in the child Runtime.
func (s *Supervisor) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ownership, err := AcquireSupervisorOwnership(s.dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = ownership.Close() }()

	presence, err := ProbeRuntimePresence(s.dbPath)
	if err != nil {
		return fmt.Errorf("failed to probe Runtime presence: %w", err)
	}
	readyEmitted := false
	if presence == RuntimePresent {
		s.emitReady(SupervisorReadyInfo{RuntimeState: SupervisorRuntimeStateAlreadyRunning})
		readyEmitted = true
	}

	backoff := NewRestartBackoff(s.restartPolicy)
	restartCount := 0
	var owned *managedRuntime
	for {
		if err := ctx.Err(); err != nil {
			if owned != nil {
				s.stopOwnedRuntime(owned)
			}
			return nil
		}

		if owned == nil {
			presence, err = ProbeRuntimePresence(s.dbPath)
			if err != nil {
				s.logger("[supervisor] Runtime presence probe failed: %v", err)
				if !waitContext(ctx, s.probeInterval) {
					return nil
				}
				continue
			}
			if presence == RuntimePresent {
				if !readyEmitted {
					s.emitReady(SupervisorReadyInfo{RuntimeState: SupervisorRuntimeStateAlreadyRunning})
					readyEmitted = true
				}
				if !waitContext(ctx, s.probeInterval) {
					return nil
				}
				continue
			}

			candidate, signal, startErr := s.startRuntime(ctx)
			if startErr != nil {
				s.logger("[supervisor] Runtime start failed; retrying with bounded backoff: %v", startErr)
				if !waitContext(ctx, backoff.NextDelay()) {
					return nil
				}
				continue
			}
			if signal.Type == RuntimeAlreadyRunningSignalType {
				_ = candidate.wait()
				if !readyEmitted {
					if current, probeErr := ProbeRuntimePresence(s.dbPath); probeErr == nil && current == RuntimePresent {
						s.emitReady(SupervisorReadyInfo{RuntimeState: SupervisorRuntimeStateAlreadyRunning})
						readyEmitted = true
					}
				}
				continue
			}

			owned = candidate
			if !readyEmitted {
				s.emitReady(SupervisorReadyInfo{
					RuntimeState: SupervisorRuntimeStateStarted,
					RuntimePID:   signal.PID,
				})
				readyEmitted = true
			} else {
				restartCount++
				if s.onRuntimeRestarted != nil {
					s.onRuntimeRestarted(SupervisorRuntimeRestartInfo{
						RuntimePID:   signal.PID,
						RestartCount: restartCount,
					})
				}
			}

			select {
			case <-ctx.Done():
				s.stopOwnedRuntime(owned)
				return nil
			case <-owned.waitCh:
				if time.Since(owned.startedAt) >= s.restartPolicy.StableAfter {
					backoff.Reset()
				}
				s.logger("[supervisor] owned Runtime PID %d exited; restart scheduled", owned.pid)
				owned = nil
				if !waitContext(ctx, backoff.NextDelay()) {
					return nil
				}
			}
		}
	}
}

func (s *Supervisor) emitReady(info SupervisorReadyInfo) {
	if s.onReady != nil {
		s.onReady(info)
	}
}

type managedRuntime struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	pid       int
	waitCh    chan error
	startedAt time.Time
}

func (s *Supervisor) startRuntime(ctx context.Context) (*managedRuntime, RuntimeSignal, error) {
	args := []string{"--db", s.dbPath}
	if os.Getenv("PROXYLENS_E2E_MODE") == "1" {
		args = append(args,
			"--connections-interval", "50",
			"--accounting-interval", "50ms",
			"--queue-capacity", "8",
		)
	}
	cmd := exec.Command(s.runtimeExe, args...)
	cmd.Env = environmentWithControllerOverride(os.Environ(), s.controllerURL)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, RuntimeSignal{}, fmt.Errorf("failed to create Runtime stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, RuntimeSignal{}, fmt.Errorf("failed to create Runtime stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, RuntimeSignal{}, fmt.Errorf("failed to create Runtime stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, RuntimeSignal{}, fmt.Errorf("failed to start Runtime: %w", err)
	}
	child := &managedRuntime{
		cmd:       cmd,
		stdin:     stdin,
		pid:       cmd.Process.Pid,
		waitCh:    make(chan error, 1),
		startedAt: time.Now(),
	}
	go func() { child.waitCh <- cmd.Wait() }()
	signalCh := make(chan RuntimeSignal, 1)
	go scanRuntimeSignals(stdout, signalCh)
	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	timer := time.NewTimer(s.readyTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.terminateManagedRuntime(child)
			return nil, RuntimeSignal{}, ctx.Err()
		case signal, ok := <-signalCh:
			if !ok {
				return nil, RuntimeSignal{}, fmt.Errorf("Runtime exited before emitting a readiness signal")
			}
			switch signal.Type {
			case RuntimeReadySignalType:
				if signal.PID != child.pid {
					s.terminateManagedRuntime(child)
					return nil, RuntimeSignal{}, fmt.Errorf("Runtime readiness PID did not match the exact child")
				}
				return child, signal, nil
			case RuntimeAlreadyRunningSignalType:
				return child, signal, nil
			}
		case <-child.waitCh:
			return nil, RuntimeSignal{}, fmt.Errorf("Runtime exited before readiness")
		case <-timer.C:
			s.terminateManagedRuntime(child)
			return nil, RuntimeSignal{}, fmt.Errorf("timed out waiting %s for Runtime readiness", s.readyTimeout)
		}
	}
}

func scanRuntimeSignals(reader io.Reader, signals chan<- RuntimeSignal) {
	defer close(signals)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if signal, ok := ParseRuntimeSignal(scanner.Text()); ok {
			select {
			case signals <- signal:
			default:
			}
		}
	}
}

func (s *Supervisor) stopOwnedRuntime(child *managedRuntime) {
	if child == nil {
		return
	}
	_, _ = child.stdin.Write([]byte("STOP\n"))
	timer := time.NewTimer(s.stopTimeout)
	defer timer.Stop()
	select {
	case <-child.waitCh:
		return
	case <-timer.C:
		s.terminateManagedRuntime(child)
	}
}

func (s *Supervisor) terminateManagedRuntime(child *managedRuntime) {
	if child == nil || child.cmd == nil || child.cmd.Process == nil {
		return
	}
	_ = child.cmd.Process.Kill()
	select {
	case <-child.waitCh:
	case <-time.After(s.stopTimeout):
	}
}

func (m *managedRuntime) wait() error {
	if m == nil {
		return nil
	}
	return <-m.waitCh
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func environmentWithControllerOverride(environment []string, controllerURL string) []string {
	if strings.TrimSpace(controllerURL) == "" {
		return environment
	}
	result := make([]string, 0, len(environment)+1)
	replaced := false
	for _, entry := range environment {
		if strings.HasPrefix(entry, ControllerURLEnv+"=") {
			if !replaced {
				result = append(result, ControllerURLEnv+"="+controllerURL)
				replaced = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !replaced {
		result = append(result, ControllerURLEnv+"="+controllerURL)
	}
	return result
}

// ResolveRuntimeExecutable applies --runtime-exe > PROXYLENS_RUNTIME_EXE >
// sibling-of-supervisor without searching PATH or guessing source-tree paths.
func ResolveRuntimeExecutable(cliValue, envValue, supervisorExecutable string) (string, error) {
	for _, candidate := range []string{cliValue, envValue} {
		if value := strings.TrimSpace(candidate); value != "" {
			return requireRegularExecutable(value)
		}
	}
	if strings.TrimSpace(supervisorExecutable) == "" {
		return "", fmt.Errorf("cannot derive Runtime executable without Supervisor executable")
	}
	supervisorExecutable, err := filepath.Abs(supervisorExecutable)
	if err != nil {
		return "", fmt.Errorf("failed to normalize Supervisor executable: %w", err)
	}
	base := filepath.Base(supervisorExecutable)
	const prefix = "proxylens-supervisor"
	if !strings.HasPrefix(strings.ToLower(base), prefix) {
		return "", fmt.Errorf("Supervisor executable basename must begin with %s", prefix)
	}
	rest := base[len(prefix):]
	if rest != "" && rest[0] != '-' && rest[0] != '.' {
		return "", fmt.Errorf("Supervisor executable basename has an unsupported suffix")
	}
	runtimePath := filepath.Join(filepath.Dir(supervisorExecutable), "proxylens-runtime"+base[len(prefix):])
	return requireRegularExecutable(runtimePath)
}

func requireRegularExecutable(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to normalize Runtime executable: %w", err)
	}
	if !isRegularFile(absPath) {
		return "", fmt.Errorf("Runtime executable is not a regular file")
	}
	return absPath, nil
}
