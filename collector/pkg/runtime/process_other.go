//go:build !windows

package runtime

import "os/exec"

func configureRuntimeProcess(_ *exec.Cmd) {}
