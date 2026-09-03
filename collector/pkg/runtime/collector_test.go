package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCollectorRunnerCancellationIsCleanAndReportsControllerStatus(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var statusMu sync.Mutex
	var statuses []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("mock controller received unexpected method %s", r.Method)
		}
		switch r.URL.Path {
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":true,"version":"mock"}`))
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
	defer server.Close()

	runner, err := NewCollectorRunner(CollectorOptions{
		ControllerURL:       server.URL,
		ConnectionsInterval: 10,
		QueueCapacity:       4,
		InitialBackoffMs:    5,
		MaxBackoffMs:        20,
		SessionID:           "sess-runner-cancel",
		OnControllerStatus: func(status string) {
			statusMu.Lock()
			statuses = append(statuses, status)
			statusMu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewCollectorRunner failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("clean runner cancellation returned an error: %v", err)
	}
	if result == nil || !result.CleanShutdown || result.Status != "closed_clean" {
		t.Fatalf("expected clean runner result, got %+v", result)
	}
	if result.SessionID != "sess-runner-cancel" {
		t.Fatalf("unexpected session id %q", result.SessionID)
	}

	statusMu.Lock()
	defer statusMu.Unlock()
	var connected bool
	for _, status := range statuses {
		if status == "connected" {
			connected = true
		}
	}
	if !connected {
		t.Fatalf("expected connected status, got %v", statuses)
	}
}

func TestCollectorRunnerIsSingleUse(t *testing.T) {
	runner, err := NewCollectorRunner(CollectorOptions{
		ControllerURL: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("NewCollectorRunner failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = runner.Run(ctx)
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("expected second Run call to fail")
	}
}
