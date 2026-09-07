//go:build windows

package installedlifecycle

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

const (
	taskTriggerLogon          = int32(9)
	taskTriggerTime           = int32(1)
	taskActionExec            = int32(0)
	taskCreateOrUpdate        = int32(6)
	taskLogonInteractiveToken = int32(3)
	taskRunLevelLimited       = int32(0)
	taskInstancesIgnoreNew    = int32(2)
)

func registerTask(name, executable string) error {
	folderPath, taskLeaf, err := taskFolderAndLeaf(name)
	if err != nil {
		return err
	}
	executable, err = ResolveTaskExecutable(executable)
	if err != nil {
		return err
	}
	arguments, err := ResolveTaskArguments()
	if err != nil {
		return err
	}
	userID, err := currentUserID()
	if err != nil {
		return err
	}
	return withTaskService(func(service *ole.IDispatch) error {
		folder, err := getOrCreateFolder(service, folderPath)
		if err != nil {
			return err
		}
		defer folder.Release()

		definition, err := callDispatch(service, "NewTask", int32(0))
		if err != nil {
			return fmt.Errorf("failed to create Task Scheduler definition: %w", err)
		}
		defer definition.Release()

		if err := configureTaskDefinition(definition, executable, arguments, userID); err != nil {
			return err
		}
		result, err := oleutil.CallMethod(
			folder,
			"RegisterTaskDefinition",
			taskLeaf,
			definition,
			taskCreateOrUpdate,
			userID,
			nil,
			taskLogonInteractiveToken,
			nil,
		)
		clearVariant(result)
		if err != nil {
			return fmt.Errorf("failed to register exact scheduled task: %w", describeTaskError(err))
		}
		return nil
	})
}

func unregisterTask(name string) error {
	folderPath, taskLeaf, err := taskFolderAndLeaf(name)
	if err != nil {
		return err
	}
	return withTaskService(func(service *ole.IDispatch) error {
		folder, err := getFolder(service, folderPath)
		if err != nil {
			if isTaskNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to access exact Task Scheduler folder: %w", describeTaskError(err))
		}
		defer folder.Release()
		task, err := getTask(folder, taskLeaf)
		if err != nil {
			if isTaskNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to access exact scheduled task: %w", describeTaskError(err))
		}
		// getTask returns an AddRef'ed dispatch. Release it before deletion;
		// the exact operation remains a single task-name DeleteTask call.
		task.Release()
		result, err := oleutil.CallMethod(folder, "DeleteTask", taskLeaf, int32(0))
		clearVariant(result)
		if err != nil && !isTaskNotFound(err) {
			return fmt.Errorf("failed to unregister exact scheduled task: %w", err)
		}
		return nil
	})
}

func statusTask(name string) (TaskStatus, error) {
	folderPath, taskLeaf, err := taskFolderAndLeaf(name)
	if err != nil {
		return TaskStatus{}, err
	}
	status := TaskStatus{TaskName: name}
	err = withTaskService(func(service *ole.IDispatch) error {
		folder, err := getFolder(service, folderPath)
		if err != nil {
			if isTaskNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to access exact Task Scheduler folder: %w", describeTaskError(err))
		}
		defer folder.Release()
		task, err := getTask(folder, taskLeaf)
		if err != nil {
			if isTaskNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to access exact scheduled task: %w", describeTaskError(err))
		}
		defer task.Release()
		status.Registered = true
		status.Enabled, err = getBool(task, "Enabled")
		if err != nil {
			return fmt.Errorf("failed to read scheduled task enabled state: %w", err)
		}
		action, err := firstAction(task)
		if err != nil {
			return fmt.Errorf("failed to read exact scheduled task action: %w", err)
		}
		defer action.Release()
		status.ActionPath, err = getString(action, "Path")
		if err != nil {
			return fmt.Errorf("failed to read exact scheduled task action path: %w", err)
		}
		return nil
	})
	return status, err
}

func runTask(name string) error {
	folderPath, taskLeaf, err := taskFolderAndLeaf(name)
	if err != nil {
		return err
	}
	return withTaskService(func(service *ole.IDispatch) error {
		folder, err := getFolder(service, folderPath)
		if err != nil {
			return err
		}
		defer folder.Release()
		task, err := getTask(folder, taskLeaf)
		if err != nil {
			return fmt.Errorf("failed to find exact scheduled task: %w", err)
		}
		defer task.Release()
		result, err := oleutil.CallMethod(task, "Run", nil)
		clearVariant(result)
		if err != nil {
			return fmt.Errorf("failed to start exact scheduled task: %w", err)
		}
		return nil
	})
}

func configureTaskDefinition(definition *ole.IDispatch, executable, arguments, userID string) error {
	registrationInfo, err := getPropertyDispatch(definition, "RegistrationInfo")
	if err != nil {
		return fmt.Errorf("failed to access Task Scheduler registration info: %w", err)
	}
	if err := putProperty(registrationInfo, "Description", "ProxyLens installed background Supervisor"); err != nil {
		registrationInfo.Release()
		return fmt.Errorf("failed to configure Task Scheduler description: %w", err)
	}
	registrationInfo.Release()

	settings, err := getPropertyDispatch(definition, "Settings")
	if err != nil {
		return fmt.Errorf("failed to access Task Scheduler settings: %w", err)
	}
	for property, value := range map[string]interface{}{
		"Enabled":                    true,
		"StartWhenAvailable":         true,
		"StopIfGoingOnBatteries":     false,
		"DisallowStartIfOnBatteries": false,
		"ExecutionTimeLimit":         "PT0S",
		"MultipleInstances":          taskInstancesIgnoreNew,
	} {
		if err := putProperty(settings, property, value); err != nil {
			settings.Release()
			return fmt.Errorf("failed to configure Task Scheduler %s: %w", property, err)
		}
	}
	settings.Release()

	principal, err := getPropertyDispatch(definition, "Principal")
	if err != nil {
		return fmt.Errorf("failed to access Task Scheduler principal: %w", err)
	}
	for property, value := range map[string]interface{}{
		"UserId":    userID,
		"LogonType": taskLogonInteractiveToken,
		"RunLevel":  taskRunLevelLimited,
	} {
		if err := putProperty(principal, property, value); err != nil {
			principal.Release()
			return fmt.Errorf("failed to configure Task Scheduler principal %s: %w", property, err)
		}
	}
	principal.Release()

	triggers, err := getPropertyDispatch(definition, "Triggers")
	if err != nil {
		return fmt.Errorf("failed to access Task Scheduler triggers: %w", err)
	}
	trigger, err := callDispatch(triggers, "Create", taskTriggerLogon)
	triggers.Release()
	if err != nil {
		return fmt.Errorf("failed to create Task Scheduler logon trigger: %w", err)
	}
	for property, value := range map[string]interface{}{
		"Enabled": true,
		"UserId":  userID,
	} {
		if err := putProperty(trigger, property, value); err != nil {
			trigger.Release()
			return fmt.Errorf("failed to configure Task Scheduler logon trigger %s: %w", property, err)
		}
	}
	if err := configureTaskTriggerRepetition(trigger); err != nil {
		trigger.Release()
		return err
	}
	trigger.Release()
	if err := addPeriodicRecoveryTrigger(definition); err != nil {
		return err
	}

	actions, err := getPropertyDispatch(definition, "Actions")
	if err != nil {
		return fmt.Errorf("failed to access Task Scheduler actions: %w", err)
	}
	action, err := callDispatch(actions, "Create", taskActionExec)
	actions.Release()
	if err != nil {
		return fmt.Errorf("failed to create Task Scheduler executable action: %w", err)
	}
	for property, value := range map[string]interface{}{
		"Path":             executable,
		"Arguments":        arguments,
		"WorkingDirectory": filepath.Dir(executable),
	} {
		if err := putProperty(action, property, value); err != nil {
			action.Release()
			return fmt.Errorf("failed to configure Task Scheduler action %s: %w", property, err)
		}
	}
	action.Release()
	return nil
}

func configureTaskTriggerRepetition(trigger *ole.IDispatch) error {
	repetition, err := getPropertyDispatch(trigger, "Repetition")
	if err != nil {
		return fmt.Errorf("failed to access Task Scheduler trigger repetition: %w", err)
	}
	defer repetition.Release()
	for property, value := range map[string]interface{}{
		"Interval":          DefaultTaskRepetitionInterval,
		"StopAtDurationEnd": false,
	} {
		if err := putProperty(repetition, property, value); err != nil {
			return fmt.Errorf("failed to configure Task Scheduler trigger repetition %s: %w", property, err)
		}
	}
	// Duration is intentionally left unset: the PT1M repetition is indefinite.
	return nil
}

// addPeriodicRecoveryTrigger configures an indefinite PT1M TimeTrigger that
// provides periodic Supervisor recovery whenever the process is killed or
// crashes, without requiring user re-logon, reboot, or manual intervention.
// MultipleInstances=IgnoreNew ensures running instances are never duplicated.
func addPeriodicRecoveryTrigger(definition *ole.IDispatch) error {
	triggers, err := getPropertyDispatch(definition, "Triggers")
	if err != nil {
		return fmt.Errorf("failed to access Task Scheduler E2E trigger collection: %w", err)
	}
	defer triggers.Release()
	timeTrigger, err := callDispatch(triggers, "Create", taskTriggerTime)
	if err != nil {
		return fmt.Errorf("failed to create isolated E2E activation trigger: %w", err)
	}
	defer timeTrigger.Release()
	for property, value := range map[string]interface{}{
		"Enabled":       true,
		"StartBoundary": time.Now().Add(2 * time.Second).Format("2006-01-02T15:04:05"),
	} {
		if err := putProperty(timeTrigger, property, value); err != nil {
			return fmt.Errorf("failed to configure Task Scheduler E2E activation trigger %s: %w", property, err)
		}
	}
	return configureTaskTriggerRepetition(timeTrigger)
}

func firstAction(task *ole.IDispatch) (*ole.IDispatch, error) {
	definition, err := getPropertyDispatch(task, "Definition")
	if err != nil {
		return nil, err
	}
	defer definition.Release()
	actions, err := getPropertyDispatch(definition, "Actions")
	if err != nil {
		return nil, err
	}
	defer actions.Release()
	return getPropertyDispatch(actions, "Item", int32(1))
}

func withTaskService(fn func(*ole.IDispatch) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		return fmt.Errorf("%w: COM initialization failed: %v", ErrTaskLifecycle, err)
	}
	defer ole.CoUninitialize()
	unknown, err := oleutil.CreateObject("Schedule.Service")
	if err != nil {
		return fmt.Errorf("%w: Task Scheduler service unavailable: %v", ErrTaskLifecycle, err)
	}
	defer unknown.Release()
	service, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("%w: Task Scheduler dispatch unavailable: %v", ErrTaskLifecycle, err)
	}
	defer service.Release()
	result, err := oleutil.CallMethod(service, "Connect")
	clearVariant(result)
	if err != nil {
		return fmt.Errorf("%w: failed to connect to current-user Task Scheduler: %v", ErrTaskLifecycle, err)
	}
	return fn(service)
}

func getOrCreateFolder(service *ole.IDispatch, folderPath string) (*ole.IDispatch, error) {
	folder, err := getFolder(service, folderPath)
	if err == nil {
		return folder, nil
	}
	if !isTaskNotFound(err) {
		return nil, err
	}
	root, rootErr := callDispatch(service, "GetFolder", `\`)
	if rootErr != nil {
		return nil, fmt.Errorf("failed to access Task Scheduler root folder: %w", rootErr)
	}
	defer root.Release()
	leaf := strings.TrimPrefix(folderPath, `\`)
	created, createErr := callDispatch(root, "CreateFolder", leaf, nil)
	if createErr != nil {
		return nil, fmt.Errorf("failed to create exact Task Scheduler folder: %w", createErr)
	}
	return created, nil
}

func getFolder(service *ole.IDispatch, path string) (*ole.IDispatch, error) {
	return callDispatch(service, "GetFolder", path)
}

func getTask(folder *ole.IDispatch, leaf string) (*ole.IDispatch, error) {
	return callDispatch(folder, "GetTask", leaf)
}

func callDispatch(dispatch *ole.IDispatch, method string, params ...interface{}) (*ole.IDispatch, error) {
	result, err := oleutil.CallMethod(dispatch, method, params...)
	if err != nil {
		clearVariant(result)
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("Task Scheduler method %s returned no object", method)
	}
	child := result.ToIDispatch()
	if child == nil {
		clearVariant(result)
		return nil, fmt.Errorf("Task Scheduler method %s returned an invalid object", method)
	}
	child.AddRef()
	clearVariant(result)
	return child, nil
}

func getPropertyDispatch(dispatch *ole.IDispatch, property string, params ...interface{}) (*ole.IDispatch, error) {
	result, err := oleutil.GetProperty(dispatch, property, params...)
	if err != nil {
		clearVariant(result)
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("Task Scheduler property %s returned no object", property)
	}
	child := result.ToIDispatch()
	if child == nil {
		clearVariant(result)
		return nil, fmt.Errorf("Task Scheduler property %s returned an invalid object", property)
	}
	child.AddRef()
	clearVariant(result)
	return child, nil
}

func putProperty(dispatch *ole.IDispatch, property string, value interface{}) error {
	result, err := oleutil.PutProperty(dispatch, property, value)
	clearVariant(result)
	return err
}

func getBool(dispatch *ole.IDispatch, property string) (bool, error) {
	result, err := oleutil.GetProperty(dispatch, property)
	if err != nil {
		clearVariant(result)
		return false, err
	}
	defer clearVariant(result)
	if result == nil {
		return false, fmt.Errorf("Task Scheduler property %s returned no value", property)
	}
	value, ok := result.Value().(bool)
	if !ok {
		return false, fmt.Errorf("Task Scheduler property %s returned a non-boolean value", property)
	}
	return value, nil
}

func getString(dispatch *ole.IDispatch, property string) (string, error) {
	result, err := oleutil.GetProperty(dispatch, property)
	if err != nil {
		clearVariant(result)
		return "", err
	}
	defer clearVariant(result)
	if result == nil {
		return "", fmt.Errorf("Task Scheduler property %s returned no value", property)
	}
	return result.ToString(), nil
}

func clearVariant(result *ole.VARIANT) {
	if result != nil {
		_ = result.Clear()
	}
}

func currentUserID() (string, error) {
	size := uint32(256)
	for attempt := 0; attempt < 4; attempt++ {
		buffer := make([]uint16, size)
		err := windows.GetUserNameEx(windows.NameSamCompatible, &buffer[0], &size)
		if err == nil {
			value := strings.TrimSpace(windows.UTF16ToString(buffer[:size]))
			if value != "" {
				return value, nil
			}
			return "", fmt.Errorf("%w: current user identity is blank", ErrTaskLifecycle)
		}
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || errors.Is(err, windows.ERROR_MORE_DATA) {
			size *= 2
			continue
		}
		return "", fmt.Errorf("%w: failed to resolve current user identity: %v", ErrTaskLifecycle, err)
	}
	return "", fmt.Errorf("%w: current user identity is too long", ErrTaskLifecycle)
}

func isTaskNotFound(err error) bool {
	if err == nil {
		return false
	}
	var oleErr *ole.OleError
	if errors.As(err, &oleErr) {
		switch oleErr.Code() {
		case 0x8004130F, 0x8004130B, 0x80070002, 0x80070003, 0x80020009:
			return true
		}
	}
	return false
}

func describeTaskError(err error) error {
	if err == nil {
		return nil
	}
	var oleErr *ole.OleError
	if errors.As(err, &oleErr) {
		return fmt.Errorf("HRESULT 0x%08X: %w", uint32(oleErr.Code()), err)
	}
	return err
}
