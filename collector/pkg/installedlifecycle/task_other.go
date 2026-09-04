//go:build !windows

package installedlifecycle

import "fmt"

func registerTask(name, executable string) error {
	return fmt.Errorf("%w: Windows Task Scheduler is unavailable", ErrTaskLifecycle)
}

func unregisterTask(name string) error {
	return fmt.Errorf("%w: Windows Task Scheduler is unavailable", ErrTaskLifecycle)
}

func statusTask(name string) (TaskStatus, error) {
	return TaskStatus{}, fmt.Errorf("%w: Windows Task Scheduler is unavailable", ErrTaskLifecycle)
}

func runTask(name string) error {
	return fmt.Errorf("%w: Windows Task Scheduler is unavailable", ErrTaskLifecycle)
}
