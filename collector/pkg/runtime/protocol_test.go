package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeProtocolParsesOnlyDocumentedSignals(t *testing.T) {
	ready, ok := ParseRuntimeSignal(`{"type":"proxylens-runtime-ready","runtimeVersion":"v1","pid":1234}`)
	if !ok || ready.Type != RuntimeReadySignalType || ready.RuntimeVersion != "v1" || ready.PID != 1234 {
		t.Fatalf("unexpected READY parse: %#v, ok=%t", ready, ok)
	}

	already, ok := ParseRuntimeSignal(`{"type":"proxylens-runtime-already-running","runtimeVersion":"v1"}`)
	if !ok || already.Type != RuntimeAlreadyRunningSignalType || already.PID != 0 {
		t.Fatalf("unexpected ALREADY parse: %#v, ok=%t", already, ok)
	}

	for _, line := range []string{
		"[runtime] ordinary log",
		"{not-json}",
		`{"type":"proxylens-runtime-ready","runtimeVersion":"v1","pid":0}`,
		`{"type":"proxylens-runtime-ready","runtimeVersion":"v1"}`,
		`{"type":"proxylens-runtime-already-running","runtimeVersion":"v1","pid":1234}`,
		`{"type":"proxylens-runtime-ready","runtimeVersion":"v1","pid":1234,"secret":"unexpected"}`,
		`{"type":"proxylens-runtime-ready","runtimeVersion":"v1","pid":1234} trailing`,
	} {
		if signal, ok := ParseRuntimeSignal(line); ok {
			t.Errorf("unsafe or unrelated line parsed as %#v: %q", signal, line)
		}
	}
}

func TestRuntimeProtocolEncodersDoNotEmitSecretFields(t *testing.T) {
	var ready bytes.Buffer
	if err := EncodeRuntimeReady(&ready, "v1", 1234); err != nil {
		t.Fatalf("EncodeRuntimeReady failed: %v", err)
	}
	if strings.Contains(ready.String(), "secret") || strings.Contains(ready.String(), "token") {
		t.Fatalf("Runtime READY contains a sensitive field: %s", ready.String())
	}

	var supervisor bytes.Buffer
	if err := EncodeSupervisorReady(&supervisor, SupervisorVersion, 12, SupervisorReadyInfo{
		RuntimeState: SupervisorRuntimeStateStarted,
		RuntimePID:   34,
	}); err != nil {
		t.Fatalf("EncodeSupervisorReady failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(supervisor.Bytes(), &payload); err != nil {
		t.Fatalf("invalid Supervisor JSON: %v", err)
	}
	if _, exists := payload["secret"]; exists {
		t.Fatal("Supervisor READY contains a secret field")
	}
	if _, exists := payload["token"]; exists {
		t.Fatal("Supervisor READY contains a token field")
	}
}

func TestListenForExactStopIgnoresOtherLinesAndEOF(t *testing.T) {
	stops := 0
	ListenForExactStop(strings.NewReader("STOPPING\n stop\nSTOP \nother\n"), func() { stops++ })
	if stops != 0 {
		t.Fatalf("non-exact STOP input cancelled Runtime %d times", stops)
	}

	ListenForExactStop(strings.NewReader("log\nSTOP\nignored\n"), func() { stops++ })
	if stops != 1 {
		t.Fatalf("exact STOP did not cancel exactly once: %d", stops)
	}

	ListenForExactStop(strings.NewReader(""), func() { stops++ })
	if stops != 1 {
		t.Fatalf("stdin EOF incorrectly cancelled Runtime: %d", stops)
	}
}

func TestSupervisorHandshakeValidation(t *testing.T) {
	var buffer bytes.Buffer
	if err := EncodeSupervisorReady(&buffer, SupervisorVersion, 1, SupervisorReadyInfo{
		RuntimeState: SupervisorRuntimeStateAlreadyRunning,
	}); err != nil {
		t.Fatalf("already-running Supervisor handshake failed: %v", err)
	}
	if strings.Contains(buffer.String(), "runtimePid") {
		t.Fatalf("already-running handshake unexpectedly included runtimePid: %s", buffer.String())
	}

	if err := EncodeSupervisorReady(&buffer, SupervisorVersion, 1, SupervisorReadyInfo{
		RuntimeState: SupervisorRuntimeStateStarted,
	}); err == nil {
		t.Fatal("started Supervisor handshake without Runtime PID unexpectedly succeeded")
	}
	if err := EncodeSupervisorRuntimeRestarted(&buffer, SupervisorVersion, 1, SupervisorRuntimeRestartInfo{}); err == nil {
		t.Fatal("invalid Runtime restart handshake unexpectedly succeeded")
	}
}

func TestSupervisorStartingHandshakeConfirmsOwnershipWithoutRuntimePID(t *testing.T) {
	var buffer bytes.Buffer
	if err := EncodeSupervisorReady(&buffer, SupervisorVersion, 1, SupervisorReadyInfo{
		RuntimeState: SupervisorRuntimeStateStarting,
	}); err != nil {
		t.Fatalf("starting Supervisor handshake failed: %v", err)
	}
	if strings.Contains(buffer.String(), "runtimePid") {
		t.Fatalf("starting handshake unexpectedly included runtimePid: %s", buffer.String())
	}
	if err := EncodeSupervisorReady(&buffer, SupervisorVersion, 1, SupervisorReadyInfo{
		RuntimeState: SupervisorRuntimeStateStarting,
		RuntimePID:   34,
	}); err == nil {
		t.Fatal("starting Supervisor handshake with Runtime PID unexpectedly succeeded")
	}
}
