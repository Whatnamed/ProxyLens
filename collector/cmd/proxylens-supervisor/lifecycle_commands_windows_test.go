//go:build windows

package main

import (
	"path/filepath"
	"testing"

	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
)

func TestControlStopReprobeKeepsExternalRuntimeVisible(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "authority.db")
	runtimeOwner, err := proxylensruntime.AcquireRuntimeOwnership(dbPath)
	if err != nil {
		t.Fatalf("failed to create isolated external Runtime owner: %v", err)
	}
	defer runtimeOwner.Close()

	status, err := controlStatusAfterSupervisorStop(dbPath)
	if err != nil {
		t.Fatalf("post-stop Runtime re-probe failed: %v", err)
	}
	if status.SupervisorRunning || !status.RuntimeRunning {
		t.Fatalf("post-stop status = %+v, want supervisor=false/runtime=true", status)
	}

	presence, err := proxylensruntime.ProbeRuntimePresence(dbPath)
	if err != nil {
		t.Fatalf("external Runtime probe failed: %v", err)
	}
	if presence != proxylensruntime.RuntimePresent {
		t.Fatalf("external Runtime presence = %q after stop status probe, want present", presence)
	}
}

func TestControlStopReprobeReportsOwnedRuntimeAbsent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "authority.db")
	status, err := controlStatusAfterSupervisorStop(dbPath)
	if err != nil {
		t.Fatalf("post-stop Runtime re-probe failed: %v", err)
	}
	if status.SupervisorRunning || status.RuntimeRunning {
		t.Fatalf("post-stop status = %+v, want supervisor=false/runtime=false", status)
	}
}
