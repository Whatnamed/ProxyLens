package test

import (
	"context"
	"database/sql"
	"errors"
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
