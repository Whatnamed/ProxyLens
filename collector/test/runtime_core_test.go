package test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/gorilla/websocket"
)

type mockControllerRequests struct {
	mu      sync.Mutex
	methods []string
	paths   []string
}

func (r *mockControllerRequests) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = append(r.methods, req.Method)
	r.paths = append(r.paths, req.URL.Path)
}

func (r *mockControllerRequests) snapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.methods...), append([]string(nil), r.paths...)
}

func TestRuntimeCoreMockControllerAccountingAndReadonlyQuery(t *testing.T) {
	requests := &mockControllerRequests{}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.record(req)
		if req.Method != http.MethodGet {
			t.Errorf("mock controller received mutating request: %s %s", req.Method, req.URL.Path)
		}
		switch req.URL.Path {
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":true,"version":"mock-runtime"}`))
		case "/connections":
			conn, err := upgrader.Upgrade(w, req, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			frames := []string{
				`{"uploadTotal":15,"downloadTotal":27,"connections":[{"id":"proxy-1","metadata":{"network":"tcp","destinationIP":"203.0.113.10","destinationPort":"443","host":"example.test","process":"mock-app.exe","processPath":"C:\\Mock\\mock-app.exe"},"upload":10,"download":20,"start":"2026-09-04T00:00:00Z","chains":["Mock-Node","Proxy-Group"],"rule":"MATCH","rulePayload":"MATCH"},{"id":"direct-1","metadata":{"network":"udp","destinationIP":"192.0.2.10","destinationPort":"123","host":"ntp.test","process":"mock-clock.exe","processPath":"C:\\Mock\\mock-clock.exe"},"upload":5,"download":7,"start":"2026-09-04T00:00:00Z","chains":["DIRECT"],"rule":"NETWORK,udp","rulePayload":"udp"}]}`,
				`{"uploadTotal":75,"downloadTotal":147,"connections":[{"id":"proxy-1","metadata":{"network":"tcp","destinationIP":"203.0.113.10","destinationPort":"443","host":"example.test","process":"mock-app.exe","processPath":"C:\\Mock\\mock-app.exe"},"upload":50,"download":90,"start":"2026-09-04T00:00:00Z","chains":["Mock-Node","Proxy-Group"],"rule":"MATCH","rulePayload":"MATCH"},{"id":"direct-1","metadata":{"network":"udp","destinationIP":"192.0.2.10","destinationPort":"123","host":"ntp.test","process":"mock-clock.exe","processPath":"C:\\Mock\\mock-clock.exe"},"upload":25,"download":27,"start":"2026-09-04T00:00:00Z","chains":["DIRECT"],"rule":"NETWORK,udp","rulePayload":"udp"}]}`,
			}
			for _, frame := range frames {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	for _, endpoint := range forbiddenControllerEndpoints() {
		if strings.Contains(server.URL, endpoint) {
			t.Fatalf("integration test unexpectedly targeted the real controller endpoint: %s", server.URL)
		}
	}
	dbPath := t.TempDir() + string(os.PathSeparator) + "runtime.db"
	var readyCount atomic.Int32
	rt, err := proxylensruntime.NewRuntime(proxylensruntime.RuntimeOptions{
		DBPath: dbPath,
		Collector: proxylensruntime.CollectorOptions{
			ControllerURL:       server.URL,
			ConnectionsInterval: 10,
			QueueCapacity:       16,
			InitialBackoffMs:    5,
			MaxBackoffMs:        20,
			SessionID:           "sess-runtime-e2e",
		},
		AccountingInterval: 20 * time.Millisecond,
		OnReady: func(info proxylensruntime.RuntimeReadyInfo) {
			if info.RuntimeVersion != proxylensruntime.RuntimeVersion {
				t.Errorf("unexpected runtime ready version %q", info.RuntimeVersion)
			}
			readyCount.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	runtimeCtx, cancelRuntime := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRuntime()
	runtimeDone := make(chan struct {
		result *proxylensruntime.RuntimeResult
		err    error
	}, 1)
	go func() {
		result, err := rt.Run(runtimeCtx)
		runtimeDone <- struct {
			result *proxylensruntime.RuntimeResult
			err    error
		}{result: result, err: err}
	}()

	var roDB *sql.DB
	var freshness *storage.AccountingFreshness
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if roDB == nil {
			candidate, openErr := storage.OpenReadOnlyDB(context.Background(), dbPath)
			if openErr == nil {
				roDB = candidate
			}
		}
		if roDB != nil {
			freshness, err = storage.NewAnalyticsService(roDB).GetAccountingFreshness(context.Background())
			if err == nil && freshness.IsFresh && freshness.CurrentJournalSequenceMax > 0 && freshness.RunID != "" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if roDB == nil {
		t.Fatal("runtime never produced a readable SQLite database")
	}
	roDB.Close()
	if freshness == nil || !freshness.IsFresh || freshness.CurrentJournalSequenceMax == 0 {
		t.Fatalf("expected fresh accounting, got %+v", freshness)
	}

	duplicate, err := proxylensruntime.NewRuntime(proxylensruntime.RuntimeOptions{
		DBPath: dbPath,
		Collector: proxylensruntime.CollectorOptions{
			ControllerURL: server.URL,
		},
	})
	if err != nil {
		t.Fatalf("duplicate NewRuntime failed: %v", err)
	}
	if _, err := duplicate.Run(context.Background()); !errors.Is(err, proxylensruntime.ErrRuntimeAlreadyRunning) {
		t.Fatalf("expected duplicate Runtime to return typed already-running error, got %v", err)
	}

	cancelRuntime()
	var runtimeResult *proxylensruntime.RuntimeResult
	select {
	case outcome := <-runtimeDone:
		runtimeResult = outcome.result
		if outcome.err != nil {
			t.Fatalf("runtime shutdown returned an error: %v", outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not shut down after cancellation")
	}
	if runtimeResult == nil || runtimeResult.Collector == nil || !runtimeResult.Collector.CleanShutdown {
		t.Fatalf("expected clean runtime result, got %+v", runtimeResult)
	}
	if readyCount.Load() != 1 {
		t.Fatalf("expected exactly one Runtime READY callback, got %d", readyCount.Load())
	}

	reopened, err := storage.OpenReadOnlyDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen read-only DB failed: %v", err)
	}
	defer reopened.Close()
	query := storage.NewQueryService(reopened)
	connections, err := query.ListConnections(context.Background(), storage.ConnectionFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query connections failed: %v", err)
	}
	if len(connections) != 2 {
		t.Fatalf("expected proxy and direct connections, got %d", len(connections))
	}
	analytics := storage.NewAnalyticsService(reopened)
	summary, err := analytics.GetUsageSummary(context.Background(), storage.AnalyticsFilter{})
	if err != nil {
		t.Fatalf("query usage summary failed: %v", err)
	}
	if summary.ProxyUpload != 40 || summary.ProxyDownload != 70 {
		t.Fatalf("unexpected proxy accounting totals: up=%d down=%d", summary.ProxyUpload, summary.ProxyDownload)
	}
	if summary.DirectUpload != 20 || summary.DirectDownload != 20 {
		t.Fatalf("unexpected direct accounting totals: up=%d down=%d", summary.DirectUpload, summary.DirectDownload)
	}
	session, err := query.GetLatestSession(context.Background())
	if err != nil {
		t.Fatalf("query latest session failed: %v", err)
	}
	if session.SessionID != "sess-runtime-e2e" || session.Status != storage.SessionStatusClosedClean {
		t.Fatalf("expected clean session closure, got %+v", session)
	}
	var sessionCount int
	if err := reopened.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM collector_sessions").Scan(&sessionCount); err != nil {
		t.Fatalf("count collector sessions failed: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("expected duplicate Runtime not to create a second collector session, got %d", sessionCount)
	}

	methods, paths := requests.snapshot()
	if len(methods) == 0 {
		t.Fatal("mock controller received no requests")
	}
	for i, method := range methods {
		if method != http.MethodGet {
			t.Fatalf("request %d used mutating method %s on path %s", i, method, paths[i])
		}
	}
	for _, path := range paths {
		if path != "/version" && path != "/connections" {
			t.Fatalf("unexpected mock controller path %s", path)
		}
	}
}

func TestRuntimeLowDiskModeIsWriteQuiescent(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/low-disk.db"

	// Initialize a valid database.
	initSink, err := storage.OpenSQLiteSink(context.Background(), dbPath, "sess-init", "v-init")
	if err != nil {
		t.Fatalf("OpenSQLiteSink failed: %v", err)
	}
	_ = initSink.EndSession(context.Background(), "sess-init", storage.SessionStatusClosedClean)
	_ = initSink.Close()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connections" {
			t.Errorf("collector MUST NOT connect to /connections in low-disk mode")
		}
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"mock"}`))
		}
	}))
	defer mockServer.Close()

	var logs []string
	var logsMu sync.Mutex
	logger := func(format string, args ...any) {
		logsMu.Lock()
		logs = append(logs, strings.TrimSpace(format))
		logsMu.Unlock()
	}

	readyCh := make(chan struct{})
	rt, err := proxylensruntime.NewRuntime(proxylensruntime.RuntimeOptions{
		DBPath: dbPath,
		Collector: proxylensruntime.CollectorOptions{
			ControllerURL: mockServer.URL,
			SessionID:     "sess-low-disk",
		},
		Logger: logger,
		OnReady: func(info proxylensruntime.RuntimeReadyInfo) {
			close(readyCh)
		},
		DiskGuardCheck: func(dbPath string) storage.DiskGuardStatus {
			return storage.DiskGuardStatus{
				Tripped:     true,
				FreeBytes:   100 << 20,
				FloorBytes:  1 << 30,
				DBSizeBytes: 10 << 20,
			}
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		_, rerr := rt.Run(ctx)
		runDone <- rerr
	}()

	// 1. Runtime must become READY for read-only queries despite degraded disk status.
	select {
	case <-readyCh:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("runtime did not become ready in low-disk mode")
	}

	// 2. Query service must work read-only.
	roDB, err := storage.OpenReadOnlyDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnlyDB failed: %v", err)
	}
	meta, err := storage.NewAnalyticsService(roDB).GetAccountingFreshness(context.Background())
	_ = roDB.Close()
	if err != nil {
		t.Fatalf("GetAccountingFreshness on low-disk DB failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected non-nil freshness from read-only service")
	}

	// 3. Graceful shutdown.
	cancel()
	select {
	case rerr := <-runDone:
		if rerr != nil {
			t.Fatalf("Run returned error on shutdown: %v", rerr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not shut down cleanly")
	}

	// 4. Verify log statements confirming pre-start write-quiescence (no DB init).
	logsMu.Lock()
	defer logsMu.Unlock()
	hasTrippedLog := false
	hasShutdownLog := false
	for _, l := range logs {
		if strings.Contains(l, "disk guard floor breached before DB init") {
			hasTrippedLog = true
		}
		if strings.Contains(l, "low-disk mode: graceful shutdown requested") {
			hasShutdownLog = true
		}
	}
	if !hasTrippedLog {
		t.Errorf("missing degraded pre-start log in logs: %v", logs)
	}
	if !hasShutdownLog {
		t.Errorf("missing graceful shutdown log in logs: %v", logs)
	}
}

// TestRuntimeLowDiskPreStartBlocksPendingMigrationsAndWrites proves that when
// free space is below the floor prior to startup, the runtime DOES NOT open the
// writer DB, DOES NOT execute pending schema migrations, and DOES NOT write to disk,
// while still becoming READY for read-only query access.
func TestRuntimeLowDiskPreStartBlocksPendingMigrationsAndWrites(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/pending-migrations.db"

	// 1. Manually initialize a database that only has migration 1 recorded
	// (so migrations 2 through 9 are pending).
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw sqlite: %v", err)
	}
	if _, err := rawDB.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT, applied_at TEXT);
		INSERT INTO schema_migrations VALUES (1, '001_initial.sql', '2026-08-25T00:00:00Z');
	`); err != nil {
		t.Fatalf("failed to insert migration 1: %v", err)
	}
	_ = rawDB.Close()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"mock"}`))
		}
	}))
	defer mockServer.Close()

	readyCh := make(chan struct{})
	rt, err := proxylensruntime.NewRuntime(proxylensruntime.RuntimeOptions{
		DBPath: dbPath,
		Collector: proxylensruntime.CollectorOptions{
			ControllerURL: mockServer.URL,
			SessionID:     "sess-low-disk-precheck",
		},
		OnReady: func(info proxylensruntime.RuntimeReadyInfo) {
			close(readyCh)
		},
		DiskGuardCheck: func(dbPath string) storage.DiskGuardStatus {
			return storage.DiskGuardStatus{
				Tripped:     true,
				FreeBytes:   50 << 20,
				FloorBytes:  1 << 30,
				DBSizeBytes: 1 << 20,
			}
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		_, rerr := rt.Run(ctx)
		runDone <- rerr
	}()

	// Runtime must still report ready (for read-only queries).
	select {
	case <-readyCh:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("runtime did not become ready in pre-start low disk mode")
	}

	cancel()
	select {
	case rerr := <-runDone:
		if rerr != nil {
			t.Fatalf("Run returned error on shutdown: %v", rerr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not shut down cleanly")
	}

	// Verify that pending migrations were NOT executed. Max version must still be 1!
	checkDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer checkDB.Close()
	var maxVer int
	if err := checkDB.QueryRowContext(context.Background(), "SELECT MAX(version) FROM schema_migrations;").Scan(&maxVer); err != nil {
		t.Fatalf("failed to query max migration version: %v", err)
	}
	if maxVer != 1 {
		t.Fatalf("pre-start low-disk mode executed migrations! maxVer=%d, want 1", maxVer)
	}
}

// TestRuntimeLowDiskMidRunBreachSuppressesShutdownAccounting proves that when a disk
// breach trips during collector execution, the collector stops with DiskGuardTripped,
// the runtime engages write-quiescence, and shutdown flush and truncate are skipped.
func TestRuntimeLowDiskMidRunBreachSuppressesShutdownAccounting(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/mid-run-disk.db"

	initSink, err := storage.OpenSQLiteSink(context.Background(), dbPath, "sess-mid-init", "v-init")
	if err != nil {
		t.Fatal(err)
	}
	_ = initSink.EndSession(context.Background(), "sess-mid-init", storage.SessionStatusClosedClean)
	_ = initSink.Close()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"mock"}`))
		case "/connections":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"uploadTotal":0,"downloadTotal":0,"connections":[]}`))
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	var logs []string
	var logsMu sync.Mutex
	logger := func(format string, args ...any) {
		logsMu.Lock()
		logs = append(logs, fmt.Sprintf(format, args...))
		logsMu.Unlock()
	}

	// Start with a guard that is NOT tripped.
	mockGuard := storage.NewDiskGuard(dbPath)
	mockGuard.SetFloorFn(func(dbSize uint64) uint64 { return 1 })

	readyCh := make(chan struct{})
	rt, err := proxylensruntime.NewRuntime(proxylensruntime.RuntimeOptions{
		DBPath: dbPath,
		Collector: proxylensruntime.CollectorOptions{
			ControllerURL: mockServer.URL,
			SessionID:     "sess-mid-run-trip",
			DiskGuard:     mockGuard,
		},
		Logger: logger,
		OnReady: func(info proxylensruntime.RuntimeReadyInfo) {
			close(readyCh)
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() {
		_, rerr := rt.Run(ctx)
		runDone <- rerr
	}()

	select {
	case <-readyCh:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not become ready")
	}

	// Trigger mid-run breach!
	mockGuard.SetFloorFn(func(dbSize uint64) uint64 { return math.MaxUint64 })
	_ = mockGuard.Check()

	// Wait for runtime to complete due to mid-run stop or shutdown
	select {
	case rerr := <-runDone:
		if rerr != nil {
			t.Fatalf("Run returned error: %v", rerr)
		}
	case <-time.After(5 * time.Second):
		// If still shutting down, cancel context to finish.
		cancel()
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
			t.Fatal("runtime did not shut down after mid-run breach")
		}
	}

	logsMu.Lock()
	defer logsMu.Unlock()
	hasMidRunLog := false
	hasSkippedLog := false
	for _, l := range logs {
		if strings.Contains(l, "collector stopped due to disk guard trip (mid-run breach)") {
			hasMidRunLog = true
		}
		if strings.Contains(l, "low-disk mode: skipped shutdown accounting flush and truncate (write-quiescent)") {
			hasSkippedLog = true
		}
	}
	if !hasMidRunLog {
		t.Errorf("missing mid-run breach log: %v", logs)
	}
	if !hasSkippedLog {
		t.Errorf("missing skipped flush/truncate log: %v", logs)
	}
}
