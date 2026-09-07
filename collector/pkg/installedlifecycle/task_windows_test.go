//go:build windows

package installedlifecycle

import (
	"path/filepath"
	"testing"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/google/uuid"
)

func TestWindowsTaskSchedulerExactTaskRoundTrip(t *testing.T) {
	t.Setenv(E2EModeEnv, "1")
	taskName := `\ProxyLens-Test\` + uuid.NewString()
	t.Setenv(E2ETaskNameEnv, taskName)
	t.Setenv(E2ETaskExecutableEnv, `C:\Windows\System32\cmd.exe`)

	if err := Register(taskName, `C:\Windows\System32\cmd.exe`); err != nil {
		t.Fatalf("isolated test task registration failed: %v", err)
	}
	defer func() {
		if err := Unregister(taskName); err != nil {
			t.Errorf("isolated test task cleanup failed: %v", err)
		}
	}()
	status, err := Status(taskName)
	if err != nil {
		t.Fatalf("isolated task status failed: %v", err)
	}
	if !status.Registered || !status.Enabled {
		t.Fatalf("isolated task status=%+v, want registered and enabled", status)
	}
	if status.ActionPath != filepath.Clean(`C:\Windows\System32\cmd.exe`) {
		t.Fatalf("isolated task action=%q, want harmless cmd.exe", status.ActionPath)
	}
	if err := Unregister(taskName); err != nil {
		t.Fatalf("isolated task unregister failed: %v", err)
	}
	status, err = Status(taskName)
	if err != nil {
		t.Fatalf("isolated task post-unregister status failed: %v", err)
	}
	if status.Registered {
		t.Fatalf("isolated task remained after exact unregister: %+v", status)
	}
}

func TestWindowsTaskSchedulerRegistersBothLogonAndTimeTriggers(t *testing.T) {
	t.Setenv(E2EModeEnv, "1")
	taskName := `\ProxyLens-Test\` + uuid.NewString()
	t.Setenv(E2ETaskNameEnv, taskName)
	t.Setenv(E2ETaskExecutableEnv, `C:\Windows\System32\cmd.exe`)

	if err := Register(taskName, `C:\Windows\System32\cmd.exe`); err != nil {
		t.Fatalf("isolated test task registration failed: %v", err)
	}
	defer func() {
		_ = Unregister(taskName)
	}()

	folderPath, taskLeaf, err := taskFolderAndLeaf(taskName)
	if err != nil {
		t.Fatalf("taskFolderAndLeaf failed: %v", err)
	}

	err = withTaskService(func(service *ole.IDispatch) error {
		folder, err := getFolder(service, folderPath)
		if err != nil {
			return err
		}
		defer folder.Release()

		task, err := getTask(folder, taskLeaf)
		if err != nil {
			return err
		}
		defer task.Release()

		definition, err := getPropertyDispatch(task, "Definition")
		if err != nil {
			return err
		}
		defer definition.Release()

		settings, err := getPropertyDispatch(definition, "Settings")
		if err != nil {
			return err
		}
		defer settings.Release()

		multiInstance, err := oleutil.GetProperty(settings, "MultipleInstances")
		if err != nil {
			return err
		}
		if multiInstance.Val != int64(taskInstancesIgnoreNew) {
			t.Fatalf("MultipleInstances = %v, want %v", multiInstance.Val, taskInstancesIgnoreNew)
		}

		triggers, err := getPropertyDispatch(definition, "Triggers")
		if err != nil {
			return err
		}
		defer triggers.Release()

		countVar, err := oleutil.GetProperty(triggers, "Count")
		if err != nil {
			return err
		}
		if countVar.Val != int64(2) {
			t.Fatalf("Triggers Count = %v, want 2 (LogonTrigger + TimeTrigger)", countVar.Val)
		}

		var hasLogon, hasTime bool
		for i := int32(1); i <= 2; i++ {
			item, err := getPropertyDispatch(triggers, "Item", i)
			if err != nil {
				return err
			}
			typeVar, err := oleutil.GetProperty(item, "Type")
			if err != nil {
				item.Release()
				return err
			}
			switch typeVar.Val {
			case int64(taskTriggerLogon):
				hasLogon = true
			case int64(taskTriggerTime):
				hasTime = true
			}
			rep, err := getPropertyDispatch(item, "Repetition")
			if err != nil {
				item.Release()
				return err
			}
			intervalVar, err := oleutil.GetProperty(rep, "Interval")
			rep.Release()
			item.Release()
			if err != nil {
				return err
			}
			if intervalVar.ToString() != DefaultTaskRepetitionInterval {
				t.Fatalf("Trigger %d Interval = %v, want %v", i, intervalVar.ToString(), DefaultTaskRepetitionInterval)
			}
		}
		if !hasLogon || !hasTime {
			t.Fatalf("missing required trigger: hasLogon=%v, hasTime=%v", hasLogon, hasTime)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withTaskService inspection failed: %v", err)
	}
}
