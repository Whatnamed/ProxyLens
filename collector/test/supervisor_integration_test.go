//go:build windows

package test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/runtimeconfig"
	"github.com/gorilla/websocket"
)

func TestSupervisorSubprocessUsesMockControllerAndSecureCredential(t *testing.T) {
	tempRoot := t.TempDir()
	supervisorBin := filepath.Join(tempRoot, "proxylens-supervisor.exe")
	runtimeBin := filepath.Join(tempRoot, "proxylens-runtime.exe")
	buildGoBinary(t, supervisorBin, "./cmd/proxylens-supervisor")
	buildGoBinary(t, runtimeBin, "./cmd/proxylens-runtime")

	const syntheticSecret = "supervisor-go-integration-secret"
	mock := newSupervisorMockController(t, syntheticSecret)
	defer mock.Close()

	credentialTarget, err := runtimeconfig.NewTestCredentialTarget()
	if err != nil {
		t.Fatalf("failed to generate random test Credential Manager target: %v", err)
	}
	store, err := runtimeconfig.NewCredentialStoreForTarget(credentialTarget)
	if err != nil {
		t.Fatalf("failed to open random test Credential Manager target: %v", err)
	}
	ctx := context.Background()
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("failed to clear random test target before use: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Delete(ctx); err != nil {
			t.Errorf("failed to delete exact random test target: %v", err)
		}
	})
	if err := store.Write(ctx, syntheticSecret); err != nil {
		t.Fatalf("failed to persist synthetic secret to random test target: %v", err)
	}

	dataDir := filepath.Join(tempRoot, "data")
	otherDataDir := filepath.Join(tempRoot, "other-data")
	configDir := filepath.Join(tempRoot, "config")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "proxylens.db")
	otherDBPath := filepath.Join(otherDataDir, "proxylens.db")
	environment := integrationEnvironment(mock.URL(), configDir, dataDir, credentialTarget)

	primary := startSupervisorProcess(t, supervisorBin, runtimeBin, dbPath, environment, "primary")
	primaryReady := waitForSupervisorJSON(t, primary.stdout, "proxylens-supervisor-ready")
	var primaryStatus struct {
		Type         string `json:"type"`
		PID          int    `json:"pid"`
		RuntimePID   int    `json:"runtimePid"`
		RuntimeState string `json:"runtimeState"`
	}
	decodeSupervisorJSON(t, primaryReady, &primaryStatus)
	if primaryStatus.PID != primary.cmd.Process.Pid || primaryStatus.RuntimePID <= 0 || primaryStatus.RuntimeState != "started" {
		t.Fatalf("unexpected primary Supervisor READY: %s", primaryReady)
	}
	if !waitForFile(dbPath, 5*time.Second) {
		t.Fatalf("primary Runtime did not create authority DB: %s", dbPath)
	}
	if !waitForMockRequests(mock, 2, 5*time.Second) {
		t.Fatalf("mock Controller did not observe the expected Runtime reads")
	}

	duplicate := startSupervisorProcess(t, supervisorBin, runtimeBin, dbPath, environment, "duplicate")
	duplicateLine := waitForSupervisorJSON(t, duplicate.stdout, "proxylens-supervisor-already-running")
	duplicateExit := waitExactIntegrationProcess(t, duplicate)
	if duplicateExit != 0 {
		t.Fatalf("duplicate Supervisor exited with %d after %s", duplicateExit, duplicateLine)
	}

	other := startSupervisorProcess(t, supervisorBin, runtimeBin, otherDBPath, environment, "other-db")
	otherReady := waitForSupervisorJSON(t, other.stdout, "proxylens-supervisor-ready")
	var otherStatus struct {
		RuntimePID   int    `json:"runtimePid"`
		RuntimeState string `json:"runtimeState"`
	}
	decodeSupervisorJSON(t, otherReady, &otherStatus)
	if otherStatus.RuntimePID <= 0 || otherStatus.RuntimeState != "started" {
		t.Fatalf("unexpected different-DB Supervisor READY: %s", otherReady)
	}
	if err := writeExactStop(other); err != nil {
		t.Fatalf("failed to send exact STOP to different-DB Supervisor: %v", err)
	}
	if exitCode := waitExactIntegrationProcess(t, other); exitCode != 0 {
		t.Fatalf("different-DB Supervisor exited with %d after exact STOP", exitCode)
	}

	if err := writeExactStop(primary); err != nil {
		t.Fatalf("failed to send exact STOP to primary Supervisor: %v", err)
	}
	if exitCode := waitExactIntegrationProcess(t, primary); exitCode != 0 {
		t.Fatalf("primary Supervisor exited with %d after exact STOP", exitCode)
	}
	if !mock.AllRequestsAuthenticatedAndReadOnly() {
		t.Fatal("mock Controller observed a non-GET or unauthenticated request")
	}
}

type supervisorProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	label  string
}

func buildGoBinary(t *testing.T, output, packagePath string) {
	t.Helper()
	collectorDir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-o", output, packagePath)
	command.Dir = collectorDir
	if outputBytes, err := command.CombinedOutput(); err != nil {
		t.Fatalf("failed to build %s: %v\n%s", packagePath, err, outputBytes)
	}
}

func integrationEnvironment(controllerURL, configDir, dataDir, credentialTarget string) []string {
	values := make([]string, 0, len(os.Environ())+5)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "MIHOMO_SECRET=") ||
			strings.HasPrefix(entry, "PROXYLENS_CONTROLLER_URL=") ||
			strings.HasPrefix(entry, "PROXYLENS_DB_PATH=") ||
			strings.HasPrefix(entry, "PROXYLENS_DATA_DIR=") ||
			strings.HasPrefix(entry, "PROXYLENS_CONFIG_DIR=") ||
			strings.HasPrefix(entry, "PROXYLENS_E2E_MODE=") ||
			strings.HasPrefix(entry, "PROXYLENS_E2E_CREDENTIAL_TARGET=") {
			continue
		}
		values = append(values, entry)
	}
	values = append(values,
		"PROXYLENS_E2E_MODE=1",
		"PROXYLENS_CONTROLLER_URL="+controllerURL,
		"PROXYLENS_CONFIG_DIR="+configDir,
		"PROXYLENS_DATA_DIR="+dataDir,
		"PROXYLENS_E2E_CREDENTIAL_TARGET="+credentialTarget,
	)
	return values
}

func startSupervisorProcess(t *testing.T, supervisorBin, runtimeBin, dbPath string, environment []string, label string) *supervisorProcess {
	t.Helper()
	command := exec.Command(supervisorBin, "--db", dbPath, "--runtime-exe", runtimeBin)
	command.Env = environment
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("%s stdout pipe failed: %v", label, err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("%s stdin pipe failed: %v", label, err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("%s Supervisor start failed: %v", label, err)
	}
	process := &supervisorProcess{cmd: command, stdin: stdin, stdout: stdout, label: label}
	t.Cleanup(func() { stopExactIntegrationProcess(process) })
	return process
}

func waitForSupervisorJSON(t *testing.T, reader io.Reader, signalType string) string {
	t.Helper()
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("Supervisor output closed before %s", signalType)
			}
			var payload struct {
				Type string `json:"type"`
			}
			if json.Unmarshal([]byte(line), &payload) == nil && payload.Type == signalType {
				return line
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", signalType)
		}
	}
}

func decodeSupervisorJSON(t *testing.T, line string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(line), target); err != nil {
		t.Fatalf("invalid Supervisor JSON %q: %v", line, err)
	}
}

func writeExactStop(process *supervisorProcess) error {
	_, err := process.stdin.Write([]byte("STOP\n"))
	return err
}

func waitExactIntegrationProcess(t *testing.T, process *supervisorProcess) int {
	t.Helper()
	if err := process.cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode()
		}
		t.Fatalf("failed waiting for %s: %v", process.label, err)
	}
	return 0
}

func stopExactIntegrationProcess(process *supervisorProcess) {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return
	}
	if process.cmd.ProcessState != nil && process.cmd.ProcessState.Exited() {
		return
	}
	_ = process.cmd.Process.Kill()
	_ = process.cmd.Wait()
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

type supervisorMockController struct {
	server *httptest.Server
	secret string
	mu     sync.Mutex
	seen   []supervisorMockRequest
}

type supervisorMockRequest struct {
	method string
	path   string
	authOK bool
}

func newSupervisorMockController(t *testing.T, secret string) *supervisorMockController {
	t.Helper()
	mock := &supervisorMockController{secret: secret}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.record(r.Method, r.URL.Path, r.Header.Get("Authorization") == "Bearer "+secret)
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"meta":true,"version":"mock-supervisor-go"}`)
		case "/connections":
			connection, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			frame := `{"uploadTotal":7,"downloadTotal":13,"connections":[]}`
			if err := connection.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				return
			}
			for {
				if _, _, err := connection.ReadMessage(); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mock.Close)
	return mock
}

func (m *supervisorMockController) record(method, path string, authOK bool) {
	m.mu.Lock()
	m.seen = append(m.seen, supervisorMockRequest{method: method, path: path, authOK: authOK})
	m.mu.Unlock()
}

func (m *supervisorMockController) URL() string {
	return m.server.URL
}

func (m *supervisorMockController) Close() {
	if m != nil && m.server != nil {
		m.server.Close()
	}
}

func (m *supervisorMockController) AllRequestsAuthenticatedAndReadOnly() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seen) == 0 {
		return false
	}
	for _, request := range m.seen {
		if request.method != http.MethodGet || !request.authOK || (request.path != "/version" && request.path != "/connections") {
			return false
		}
	}
	return true
}

func waitForMockRequests(mock *supervisorMockController, count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mock.mu.Lock()
		seen := len(mock.seen)
		mock.mu.Unlock()
		if seen >= count {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
