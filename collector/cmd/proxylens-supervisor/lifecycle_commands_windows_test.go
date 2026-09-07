//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func runControlStopWithCapture(args []string) (int, string, string) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	exitCode := controlStopCommand(args)

	_ = wOut.Close()
	_ = wErr.Close()

	var bufOut, bufErr bytes.Buffer
	_, _ = io.Copy(&bufOut, rOut)
	_, _ = io.Copy(&bufErr, rErr)
	_ = rOut.Close()
	_ = rErr.Close()

	return exitCode, bufOut.String(), bufErr.String()
}

func TestControlStopCommandNoOwnerNoRuntimeExitsZero(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "authority.db")
	exitCode, stdout, stderr := runControlStopWithCapture([]string{"--db", dbPath})
	if exitCode != 0 {
		t.Fatalf("controlStopCommand exit code = %d (stderr: %s), want 0", exitCode, stderr)
	}
	var status controlStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("failed to decode stdout JSON: %v (raw: %q)", err, stdout)
	}
	if status.SupervisorRunning || status.RuntimeRunning {
		t.Fatalf("unexpected status: %+v, want both false", status)
	}
}

func TestControlStopCommandOrphanRuntimeFailsClosed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "authority.db")
	runtimeOwner, err := proxylensruntime.AcquireRuntimeOwnership(dbPath)
	if err != nil {
		t.Fatalf("failed to acquire isolated external Runtime owner: %v", err)
	}
	defer runtimeOwner.Close()

	exitCode, stdout, stderr := runControlStopWithCapture([]string{"--db", dbPath})
	if exitCode == 0 {
		t.Fatalf("controlStopCommand unexpectedly succeeded when Runtime remains active! stdout: %s", stdout)
	}
	if exitCode != 1 {
		t.Fatalf("controlStopCommand exit code = %d, want 1", exitCode)
	}
	var status controlStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("failed to decode stdout JSON: %v (raw: %q)", err, stdout)
	}
	if status.SupervisorRunning || !status.RuntimeRunning {
		t.Fatalf("unexpected status: %+v, want supervisor=false/runtime=true", status)
	}
	if !strings.Contains(stderr, "not quiesced") {
		t.Fatalf("stderr %q missing 'not quiesced' error", stderr)
	}

	presence, err := proxylensruntime.ProbeRuntimePresence(dbPath)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if presence != proxylensruntime.RuntimePresent {
		t.Fatalf("Runtime ownership was unexpectedly cleared: %v", presence)
	}
}
