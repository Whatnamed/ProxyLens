package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/client"
	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/queue"
	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
	"github.com/gorilla/websocket"
)

func TestActiveMapMemoryReclaimSoak(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := state.NewStateEngine(state.EngineOptions{Sink: memSink})

	var conns []types.ConnectionSnapshot
	for i := 0; i < 1000; i++ {
		conns = append(conns, types.ConnectionSnapshot{
			ID:       fmt.Sprintf("soak-conn-%d", i),
			Upload:   100,
			Download: 100,
			Chains:   []string{"DIRECT"},
		})
	}

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 100000, DownloadTotal: 100000,
			Connections: conns,
		},
	}
	_ = engine.ProcessFrame(frame1)

	if engine.GetActiveConnectionsCount() != 1000 {
		t.Fatalf("Expected 1000 active connections, got %d", engine.GetActiveConnectionsCount())
	}

	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 100000, DownloadTotal: 100000,
			Connections: []types.ConnectionSnapshot{},
		},
	}
	_ = engine.ProcessFrame(frame2)

	if engine.GetActiveConnectionsCount() != 0 {
		t.Fatalf("Expected active connections to be completely reclaimed (0), got %d", engine.GetActiveConnectionsCount())
	}
}

func TestReadOnlyEnforcementInSourceCode(t *testing.T) {
	rootPath := filepath.Join("..", "pkg")
	forbiddenPatterns := []string{
		`http.NewRequest("PUT"`,
		`http.NewRequest("POST"`,
		`http.NewRequest("DELETE"`,
		`http.NewRequest("PATCH"`,
		`http.NewRequestWithContext(ctx, "PUT"`,
		`http.NewRequestWithContext(ctx, "POST"`,
		`http.NewRequestWithContext(ctx, "DELETE"`,
		`http.NewRequestWithContext(ctx, "PATCH"`,
	}

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			content, rErr := os.ReadFile(path)
			if rErr != nil {
				return rErr
			}
			sContent := string(content)
			for _, p := range forbiddenPatterns {
				if strings.Contains(sContent, p) {
					t.Errorf("Violation: Source file %s contains forbidden mutating HTTP call: %s", path, p)
				}
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to scan pkg source files: %v", err)
	}
}

func TestQueueSlowConsumerBackpressure(t *testing.T) {
	q := queue.NewBoundedQueue[int](2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	producedCount := 10
	var dequeuedItems []int
	doneCh := make(chan struct{})

	// 慢消费者
	go func() {
		defer close(doneCh)
		for {
			item, ok := q.Pop(ctx)
			if !ok {
				return
			}
			dequeuedItems = append(dequeuedItems, item)
			time.Sleep(5 * time.Millisecond)
			if len(dequeuedItems) == producedCount {
				return
			}
		}
	}()

	// 生产者通过阻塞 Push 施加 Backpressure
	for i := 0; i < producedCount; i++ {
		if err := q.Push(ctx, i); err != nil {
			t.Fatalf("Push failed on item %d: %v", i, err)
		}
	}

	<-doneCh

	if len(dequeuedItems) != producedCount {
		t.Fatalf("Expected %d dequeued items, got %d", producedCount, len(dequeuedItems))
	}

	for i := 0; i < producedCount; i++ {
		if dequeuedItems[i] != i {
			t.Errorf("Ordering violation at index %d: expected %d, got %d", i, i, dequeuedItems[i])
		}
	}
}

func TestMockControllerFullLifecycleAndReconnection(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	var wsRequestCount int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"meta":true,"version":"1.10.0"}`))
			return
		}

		if r.URL.Path == "/connections" {
			wsRequestCount++
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			if wsRequestCount == 1 {
				// 首次模拟升级成功后立即关闭触发断线
				time.Sleep(10 * time.Millisecond)
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "disconnect"), time.Now().Add(time.Second))
				return
			}

			// 第二次模拟正常推送 1 帧后结束
			frameJSON := `{"uploadTotal":100,"downloadTotal":200,"connections":[]}`
			_ = conn.WriteMessage(websocket.TextMessage, []byte(frameJSON))
			time.Sleep(50 * time.Millisecond)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cfg := &config.Config{
		ControllerURL:       ts.URL,
		ConnectionsInterval: 250,
		QueueCapacity:       10,
		InitialBackoffMs:    50,
		MaxBackoffMs:        200,
	}

	c := client.NewControllerClient(cfg)
	q := queue.NewBoundedQueue[*types.IngestItem](10)

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	go func() {
		_ = c.RunStreamLoop(ctx, q)
	}()

	var items []*types.IngestItem
	for {
		item, ok := q.Pop(ctx)
		if !ok {
			break
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		t.Fatalf("Expected to receive items from mock controller")
	}

	var hasGapOpened bool
	var hasFrame bool
	for _, it := range items {
		if it.Kind == types.ItemGapOpened {
			hasGapOpened = true
		}
		if it.Kind == types.ItemFrame {
			hasFrame = true
		}
	}

	if !hasGapOpened {
		t.Errorf("Expected ItemGapOpened to be recorded upon mock disconnect")
	}
	if !hasFrame {
		t.Errorf("Expected ItemFrame to be received after reconnect")
	}
}
