package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/installedlifecycle"
	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
)

type controlStatus struct {
	SupervisorRunning bool `json:"supervisorRunning"`
	RuntimeRunning    bool `json:"runtimeRunning"`
}

type installedOwnerStatus struct {
	Mode              string `json:"mode"`
	TaskName          string `json:"taskName"`
	TaskRegistered    bool   `json:"taskRegistered"`
	TaskEnabled       bool   `json:"taskEnabled"`
	SupervisorRunning bool   `json:"supervisorRunning"`
	RuntimeRunning    bool   `json:"runtimeRunning"`
	ActionPath        string `json:"actionPath,omitempty"`
}

func runControlCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: proxylens-supervisor control <status|stop>")
		return 2
	}
	switch args[0] {
	case "status":
		return controlStatusCommand(args[1:])
	case "stop":
		return controlStopCommand(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown control command %q\n", args[0])
		return 2
	}
}

func controlStatusCommand(args []string) int {
	dbPath, err := parseLifecycleDB(args, "control status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	presence, err := proxylensruntime.ProbeSupervisorPresence(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect Supervisor ownership: %v\n", err)
		return 1
	}
	runtimePresence, err := proxylensruntime.ProbeRuntimePresence(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect Runtime ownership: %v\n", err)
		return 1
	}
	return encodeLifecycleStatus(controlStatus{
		SupervisorRunning: presence == proxylensruntime.SupervisorPresent,
		RuntimeRunning:    runtimePresence == proxylensruntime.RuntimePresent,
	})
}

func controlStopCommand(args []string) int {
	fs := flag.NewFlagSet("control stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	db := fs.String("db", "", "Optional explicit SQLite database path")
	wait := fs.Duration("wait", 10*time.Second, "Maximum graceful-stop wait")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "control stop does not accept positional arguments")
		return 2
	}
	resolution, err := proxylensruntime.ResolveWritableDBPath(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve runtime database path: %v\n", err)
		return 1
	}
	presence, err := proxylensruntime.ProbeSupervisorPresence(resolution.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect Supervisor ownership: %v\n", err)
		return 1
	}
	if presence == proxylensruntime.SupervisorPresent {
		if err := proxylensruntime.SignalSupervisorStop(resolution.Path); err != nil {
			// The owner may have exited between probe and signal. Re-probe the
			// exact mutex before treating the race as an error.
			current, probeErr := proxylensruntime.ProbeSupervisorPresence(resolution.Path)
			if probeErr != nil || current == proxylensruntime.SupervisorPresent {
				fmt.Fprintf(os.Stderr, "Failed to signal Supervisor stop: %v\n", err)
				return 1
			}
		}
		ctx := context.Background()
		if err := proxylensruntime.WaitForSupervisorAbsence(ctx, resolution.Path, *wait); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to wait for Supervisor stop: %v\n", err)
			return 1
		}
	}
	return encodeLifecycleStatus(controlStatus{SupervisorRunning: false, RuntimeRunning: false})
}

func runInstallCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: proxylens-supervisor install <status|register|unregister|run|ensure-owner>")
		return 2
	}
	switch args[0] {
	case "status":
		return installStatusCommand(args[1:])
	case "register":
		return installRegisterCommand(args[1:])
	case "unregister":
		return installUnregisterCommand(args[1:])
	case "run":
		return installRunCommand(args[1:])
	case "ensure-owner":
		return installEnsureOwnerCommand(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown install command %q\n", args[0])
		return 2
	}
}

func installStatusCommand(args []string) int {
	dbPath, err := parseLifecycleDB(args, "install status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	name, err := installedlifecycle.ResolveTaskName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve installed owner identity: %v\n", err)
		return 1
	}
	task, err := installedlifecycle.Status(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect installed owner: %v\n", err)
		return 1
	}
	presence, err := proxylensruntime.ProbeSupervisorPresence(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect Supervisor ownership: %v\n", err)
		return 1
	}
	runtimePresence, err := proxylensruntime.ProbeRuntimePresence(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect Runtime ownership: %v\n", err)
		return 1
	}
	return encodeLifecycleStatus(installedOwnerStatus{
		Mode:              installedlifecycle.TaskOwnerModeInstalled,
		TaskName:          task.TaskName,
		TaskRegistered:    task.Registered,
		TaskEnabled:       task.Enabled,
		SupervisorRunning: presence == proxylensruntime.SupervisorPresent,
		RuntimeRunning:    runtimePresence == proxylensruntime.RuntimePresent,
		ActionPath:        task.ActionPath,
	})
}

func installRegisterCommand(args []string) int {
	if _, err := parseOptionalDB(args, "install register"); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	name, err := installedlifecycle.ResolveTaskName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve installed owner identity: %v\n", err)
		return 1
	}
	executable, err := currentSupervisorTaskExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve Supervisor task action: %v\n", err)
		return 1
	}
	if err := installedlifecycle.Register(name, executable); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register installed owner: %v\n", err)
		return 1
	}
	return 0
}

func installUnregisterCommand(args []string) int {
	if _, err := parseOptionalDB(args, "install unregister"); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	name, err := installedlifecycle.ResolveTaskName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve installed owner identity: %v\n", err)
		return 1
	}
	if err := installedlifecycle.Unregister(name); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unregister installed owner: %v\n", err)
		return 1
	}
	return 0
}

func installRunCommand(args []string) int {
	if _, err := parseOptionalDB(args, "install run"); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	name, err := installedlifecycle.ResolveTaskName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve installed owner identity: %v\n", err)
		return 1
	}
	if err := installedlifecycle.Run(name); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start installed owner: %v\n", err)
		return 1
	}
	return 0
}

func installEnsureOwnerCommand(args []string) int {
	dbPath, err := parseLifecycleDB(args, "install ensure-owner")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	name, err := installedlifecycle.ResolveTaskName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve installed owner identity: %v\n", err)
		return 1
	}
	cfg, err := loadRuntimeConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load runtime config: %v\n", err)
		return 1
	}
	if !cfg.AutostartEnabled {
		if err := installedlifecycle.Unregister(name); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to disable installed owner: %v\n", err)
			return 1
		}
		return emitInstalledOwnerStatus(name, dbPath)
	}
	executable, err := currentSupervisorTaskExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve Supervisor task action: %v\n", err)
		return 1
	}
	if err := installedlifecycle.Register(name, executable); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reconcile installed owner: %v\n", err)
		return 1
	}
	if installedlifecycle.IsE2EDirectOwner() {
		if err := startDirectSupervisor(dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start isolated E2E Supervisor owner: %v\n", err)
			return 1
		}
	} else if err := installedlifecycle.Run(name); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start installed owner: %v\n", err)
		return 1
	}
	if err := waitForSupervisorPresence(dbPath, 10*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to confirm installed Supervisor owner: %v\n", err)
		return 1
	}
	return emitInstalledOwnerStatus(name, dbPath)
}

func configSetAutostartCommand(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: proxylens-supervisor config set-autostart <true|false>")
		return 2
	}
	enabled, err := strconv.ParseBool(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "autostart value must be true or false")
		return 2
	}
	cfg, err := loadRuntimeConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load runtime config: %v\n", err)
		return 1
	}
	cfg.AutostartEnabled = enabled
	if err := saveRuntimeConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save runtime config: %v\n", err)
		return 1
	}
	if err := reconcileExistingInstalledOwner(enabled); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to reconcile installed owner preference: %v\n", err)
		return 1
	}
	return 0
}

func reconcileExistingInstalledOwner(enabled bool) error {
	name, err := installedlifecycle.ResolveTaskName()
	if err != nil {
		return err
	}
	if !enabled {
		return installedlifecycle.Unregister(name)
	}
	executable, err := currentSupervisorTaskExecutable()
	if err != nil {
		return err
	}
	return installedlifecycle.Register(name, executable)
}

func emitInstalledOwnerStatus(name, dbPath string) int {
	task, err := installedlifecycle.Status(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect installed owner after reconcile: %v\n", err)
		return 1
	}
	presence, err := proxylensruntime.ProbeSupervisorPresence(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect Supervisor ownership after reconcile: %v\n", err)
		return 1
	}
	runtimePresence, err := proxylensruntime.ProbeRuntimePresence(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect Runtime ownership after reconcile: %v\n", err)
		return 1
	}
	return encodeLifecycleStatus(installedOwnerStatus{
		Mode:              installedlifecycle.TaskOwnerModeInstalled,
		TaskName:          task.TaskName,
		TaskRegistered:    task.Registered,
		TaskEnabled:       task.Enabled,
		SupervisorRunning: presence == proxylensruntime.SupervisorPresent,
		RuntimeRunning:    runtimePresence == proxylensruntime.RuntimePresent,
		ActionPath:        task.ActionPath,
	})
}

func currentSupervisorTaskExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return installedlifecycle.ResolveTaskExecutable(executable)
}

func startDirectSupervisor(dbPath string) error {
	if !installedlifecycle.IsE2EDirectOwner() {
		return fmt.Errorf("direct Supervisor owner is restricted to isolated E2E mode")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open isolated Supervisor stdio: %w", err)
	}
	defer devNull.Close()
	command := exec.Command(executable, "--db", dbPath)
	command.Env = os.Environ()
	command.Stdin = devNull
	command.Stdout = devNull
	command.Stderr = devNull
	if err := command.Start(); err != nil {
		return fmt.Errorf("failed to start isolated Supervisor: %w", err)
	}
	_ = command.Process.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		presence, probeErr := proxylensruntime.ProbeSupervisorPresence(dbPath)
		if probeErr != nil {
			return probeErr
		}
		if presence == proxylensruntime.SupervisorPresent {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for isolated Supervisor ownership")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func waitForSupervisorPresence(dbPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		presence, err := proxylensruntime.ProbeSupervisorPresence(dbPath)
		if err != nil {
			return err
		}
		if presence == proxylensruntime.SupervisorPresent {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Supervisor ownership")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func parseLifecycleDB(args []string, commandName string) (string, error) {
	path, err := parseOptionalDB(args, commandName)
	if err != nil {
		return "", err
	}
	resolution, err := proxylensruntime.ResolveWritableDBPath(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve runtime database path: %w", err)
	}
	return resolution.Path, nil
}

func parseOptionalDB(args []string, commandName string) (string, error) {
	fs := flag.NewFlagSet(commandName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	db := fs.String("db", "", "Optional explicit SQLite database path")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() != 0 {
		return "", fmt.Errorf("%s does not accept positional arguments", commandName)
	}
	return *db, nil
}

func encodeLifecycleStatus(value any) int {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to emit lifecycle status: %v\n", err)
		return 1
	}
	return 0
}
