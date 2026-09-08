package installedlifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	ProductionTaskName            = `\ProxyLens\Background Supervisor`
	E2ETaskNameEnv                = "PROXYLENS_E2E_TASK_NAME"
	E2ETaskExecutableEnv          = "PROXYLENS_E2E_TASK_EXE"
	E2ETaskArgumentsEnv           = "PROXYLENS_E2E_TASK_ARGS"
	E2EDirectOwnerEnv             = "PROXYLENS_E2E_DIRECT_OWNER"
	E2EModeEnv                    = "PROXYLENS_E2E_MODE"
	DefaultTaskRepetitionInterval = "PT1M"
	TaskOwnerModeInstalled        = "installed-task"
	TaskOwnerModeDirect           = "direct-supervisor"
	TaskOwnerModeUnavailable      = "unavailable"
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

// ResolveTaskExecutable validates the executable selected for the exact task.
// Production callers supply the GUI-subsystem background host; the harmless
// executable override exists only for isolated E2E task-owner tests.
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

// BackgroundSupervisorExecutableName is the installed GUI-subsystem host used
// only as the Task Scheduler action. The console Supervisor CLI remains the
// lifecycle/configuration entry point for Tauri, NSIS, and developers.
func BackgroundSupervisorExecutableName() string {
	if runtime.GOOS == "windows" {
		return "proxylens-supervisor-host.exe"
	}
	return "proxylens-supervisor-host"
}

// ResolveTaskArguments is deliberately test-only. Production Task Scheduler
// actions have no arguments; isolated E2E actions may use a harmless wrapper
// command to re-establish the temporary test environment because Task
// Scheduler does not inherit the harness process environment.
func ResolveTaskArguments() (string, error) {
	value := strings.TrimSpace(os.Getenv(E2ETaskArgumentsEnv))
	if !isE2E() {
		if value != "" {
			return "", fmt.Errorf("%w: test task argument override requires %s=1", ErrInvalidTaskIdentity, E2EModeEnv)
		}
		return "", nil
	}
	if len(value) > 8192 || strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("%w: E2E task arguments are too long or contain NUL", ErrInvalidTaskIdentity)
	}
	return value, nil
}

func IsE2EDirectOwner() bool {
	return isE2E() && strings.TrimSpace(os.Getenv(E2EDirectOwnerEnv)) == "1"
}

// IsInstalledLayout is an evidence-based product-layout check. It deliberately
// requires the complete sibling set produced by the accepted NSIS layout and
// the uninstaller marker; a checkout or a copied development binary must not
// be able to mutate the production Task Scheduler owner through Settings.
func IsInstalledLayout(supervisorExecutable string) bool {
	value := strings.TrimSpace(supervisorExecutable)
	if value == "" {
		return false
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return false
	}
	directory := filepath.Dir(abs)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, name := range []string{
		"proxylens-supervisor" + ext,
		"proxylens-supervisor-host" + ext,
		"proxylens-runtime" + ext,
		"proxylens-query-api" + ext,
		"proxylens-desktop" + ext,
		"uninstall" + ext,
	} {
		info, statErr := os.Stat(filepath.Join(directory, name))
		if statErr != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
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
