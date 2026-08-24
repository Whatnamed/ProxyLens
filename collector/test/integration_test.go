package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/client"
	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/queue"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func TestReadOnlyEnforcementInSourceCode(t *testing.T) {
	forbiddenPatterns := []string{
		"PATCH /configs",
		"PUT /configs",
		"POST /restart",
		"PUT /proxies",
		"DELETE /connections",
	}

	err := filepath.Walk("../pkg", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pattern := range forbiddenPatterns {
			if strings.Contains(string(content), pattern) {
				return fmt.Errorf("forbidden mutation endpoint '%s' found in production code: %s", pattern, path)
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Read-only enforcement test failed: %v", err)
	}
}

func TestQueueBlockingPushWithTimeout(t *testing.T) {
	q := queue.NewBoundedQueue[int](2)

	// Push 2 items
	if err := q.PushWithContext(context.Background(), 1, 100*time.Millisecond); err != nil {
		t.Fatalf("Failed to push 1: %v", err)
	}
	if err := q.PushWithContext(context.Background(), 2, 100*time.Millisecond); err != nil {
		t.Fatalf("Failed to push 2: %v", err)
	}

	// 3rd item with small timeout must return ErrQueueTimeout and increment OverloadsCount
	err := q.PushWithContext(context.Background(), 3, 50*time.Millisecond)
	if err != queue.ErrQueueTimeout {
		t.Fatalf("Expected ErrQueueTimeout, got %v", err)
	}

	metrics := q.GetMetrics()
	if metrics.OverloadsCount != 1 || metrics.CurrentDepth != 2 {
		t.Errorf("Unexpected queue metrics: %+v", metrics)
	}
}

func TestMockControllerFullLifecycleAndReconnection(t *testing.T) {
	var connCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"meta":    true,
				"version": "1.10.0",
			})
			return
		}

		if strings.HasPrefix(r.URL.Path, "/connections") {
			cur := connCount.Add(1)
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking not supported", http.StatusInternalServerError)
				return
			}
			conn, bufrw, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer conn.Close()

			// 发送 HTTP 101 WebSocket Upgrade 响应
			bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
			bufrw.WriteString("Upgrade: websocket\r\n")
			bufrw.WriteString("Connection: Upgrade\r\n")
			bufrw.WriteString("Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
			bufrw.Flush()

			// 构造一个简单的快照帧
			payload := types.ConnectionSnapshotPayload{
				UploadTotal:   1000 * int64(cur),
				DownloadTotal: 2000 * int64(cur),
				Connections: []types.ConnectionSnapshot{
					{
						ID:       fmt.Sprintf("mock-conn-%d", cur),
						Upload:   100,
						Download: 200,
						Metadata: types.RawMetadata{Process: "mock.exe", Host: "test.org"},
						Rule:     "Match",
						Chains:   []string{"DIRECT"},
					},
				},
			}
			payloadBytes, _ := json.Marshal(payload)

			// 封装 RFC 6455 Text Frame
			var frame []byte
			frame = append(frame, 0x81)
			length := len(payloadBytes)
			if length <= 125 {
				frame = append(frame, byte(length))
			} else if length <= 65535 {
				frame = append(frame, 126, byte(length>>8), byte(length&0xFF))
			} else {
				frame = append(frame, 127, 0, 0, 0, 0, 0, 0, byte(length>>8), byte(length&0xFF))
			}
			frame = append(frame, payloadBytes...)
			conn.Write(frame)

			// 发送一帧后故意断开连接，模拟网络抖动/断线
			time.Sleep(50 * time.Millisecond)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{
		ControllerURL:       server.URL,
		ConnectionsInterval: 100,
		QueueCapacity:       100,
		InitialBackoffMs:    100,
		MaxBackoffMs:        500,
	}

	c := client.NewControllerClient(cfg)
	q := queue.NewBoundedQueue[*types.IngestItem](100)

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	go func() {
		_ = c.RunStreamLoop(ctx, q)
	}()

	// 等待接收至少两轮连接（证明重连机制正常生效）
	var receivedFrames int
	var receivedGaps int

	for {
		item, ok := q.Pop(ctx)
		if !ok {
			break
		}
		if item.Kind == types.ItemFrame {
			receivedFrames++
		} else if item.Kind == types.ItemGapOpened {
			receivedGaps++
		}
		if receivedFrames >= 2 && receivedGaps >= 1 {
			break
		}
	}

	if receivedFrames < 2 {
		t.Errorf("Expected at least 2 frames across reconnects, got %d", receivedFrames)
	}
	if receivedGaps < 1 {
		t.Errorf("Expected at least 1 gap item on disconnect, got %d", receivedGaps)
	}
}
