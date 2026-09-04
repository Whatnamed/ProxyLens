package installedlifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ProductionTaskName       = `\ProxyLens\Background Supervisor`
	E2ETaskNameEnv           = "PROXYLENS_E2E_TASK_NAME"
	E2ETaskExecutableEnv     = "PROXYLENS_E2E_TASK_EXE"
	E2ETaskScheduleEnv       = "PROXYLENS_E2E_TASK_SCHEDULE"
	E2EDirectOwnerEnv        = "PROXYLENS_E2E_DIRECT_OWNER"
	E2EModeEnv               = "PROXYLENS_E2E_MODE"
	DefaultRestartCount      = 10
	DefaultRestartInterval   = "PT1M"
	TaskOwnerModeInstalled   = "installed-task"
	TaskOwnerModeDirect      = "direct-supervisor"
	TaskOwnerModeUnavailable = "unavailable"
)

var (
	ErrTaskNotFound        = errors.New("ProxyLens scheduled task was not found")
	ErrTaskLifecycle       = errors.New("ProxyLens installed task lifecycle is unavailable")
	ErrInvalidTaskIdentity = errors.New("invalid ProxyLens scheduled task identity")
	testTaskNamePattern    = regexp.MustCompile(`^\\ProxyLens-Test\\[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// TaskStatus is deliberately limited to exact-task facts needed by the CLI
// and Tauri. It contains no secret, process enumeration, or task collection.
type TaskStatus struct {
	TaskName   string `json:"taskName"`
	Registered bool   `json:"taskRegistered"`
	Enabled    bool   `json:"taskEnabled"`
	ActionPath string `json:"actionPath,omitempty"`
}

// ResolveTaskName applies the ownership identity contract. Production uses a
// fixed task; only E2E can select one random ProxyLens-Test UUID task.
func ResolveTaskName() (string, error) {
	if !isE2E() {
		if override := strings.TrimSpace(os.Getenv(E2ETaskNameEnv)); override != "" {
			return "", fmt.Errorf("%w: test task override requires %s=1", ErrInvalidTaskIdentity, E2EModeEnv)
		}
		return ProductionTaskName, nil
	}
	name := strings.TrimSpace(os.Getenv(E2ETaskNameEnv))
	if name == "" {
		return "", fmt.Errorf("%w: E2E mode requires %s", ErrInvalidTaskIdentity, E2ETaskNameEnv)
	}
	if !testTaskNamePattern.MatchString(name) {
		return "", fmt.Errorf("%w: E2E task must match \\ProxyLens-Test\\<UUID>", ErrInvalidTaskIdentity)
	}
	return name, nil
}

// ResolveTaskExecutable selects the installed Supervisor by default. The
// harmless executable override exists only for isolated E2E task-owner tests.
func ResolveTaskExecutable(defaultPath string) (string, error) {
	override := strings.TrimSpace(os.Getenv(E2ETaskExecutableEnv))
	if !isE2E() {
		if override != "" {
			return "", fmt.Errorf("%w: test task executable override requires %s=1", ErrInvalidTaskIdentity, E2EModeEnv)
		}
	} else if override != "" {
		defaultPath = override
	}
	if strings.TrimSpace(defaultPath) == "" {
		return "", fmt.Errorf("%w: task action executable is required", ErrTaskLifecycle)
	}
	absolute, err := filepath.Abs(defaultPath)
	if err != nil {
		return "", fmt.Errorf("%w: failed to normalize task action executable: %v", ErrTaskLifecycle, err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: task action executable is not a regular file", ErrTaskLifecycle)
	}
	return absolute, nil
}

func IsE2EDirectOwner() bool {
	return isE2E() && strings.TrimSpace(os.Getenv(E2EDirectOwnerEnv)) == "1"
}

func isE2E() bool {
	return strings.TrimSpace(os.Getenv(E2EModeEnv)) == "1"
}

func validateTaskName(name string) error {
	value := strings.TrimSpace(name)
	if value == ProductionTaskName {
		if isE2E() {
			return fmt.Errorf("%w: E2E must not target the production task", ErrInvalidTaskIdentity)
		}
		return nil
	}
	if isE2E() && testTaskNamePattern.MatchString(value) {
		return nil
	}
	return fmt.Errorf("%w: task name is outside the allowed identity", ErrInvalidTaskIdentity)
}

func taskFolderAndLeaf(name string) (string, string, error) {
	if err := validateTaskName(name); err != nil {
		return "", "", err
	}
	index := strings.LastIndex(name, `\`)
	if index <= 0 || index == len(name)-1 {
		return "", "", fmt.Errorf("%w: task path must contain a folder and leaf", ErrInvalidTaskIdentity)
	}
	folder := name[:index]
	leaf := name[index+1:]
	if strings.ContainsAny(folder+leaf, `/`) || strings.Contains(folder, `..`) || strings.Contains(leaf, `..`) {
		return "", "", fmt.Errorf("%w: task path contains unsupported separators", ErrInvalidTaskIdentity)
	}
	return folder, leaf, nil
}

func Register(name, executable string) error {
	return registerTask(name, executable)
}

func Unregister(name string) error {
	return unregisterTask(name)
}

func Status(name string) (TaskStatus, error) {
	return statusTask(name)
}

func Run(name string) error {
	return runTask(name)
}
