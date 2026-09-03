package client

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/queue"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
	"github.com/gorilla/websocket"
)

// VersionInfo 映射 GET /version 返回
type VersionInfo struct {
	Meta    bool   `json:"meta"`
	Version string `json:"version"`
}

// ControllerClient 提供基于工业级成熟 WebSocket 库的只读 Mihomo 客户端
type ControllerClient struct {
	config                               *config.Config
	httpClient                           *http.Client
	hasConnectedBefore                   atomic.Bool
	ValidationForceDisconnectAfterFrames int
	StatusCallback                       func(status string)
}

func (c *ControllerClient) notifyStatus(status string) {
	if c.StatusCallback != nil {
		c.StatusCallback(status)
	}
}

// NewControllerClient 创建只读客户端
func NewControllerClient(cfg *config.Config) *ControllerClient {
	return &ControllerClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// CheckVersion 验证 Controller 连通性并获取版本 (只读 GET /version)
func (c *ControllerClient) CheckVersion(ctx context.Context) (*VersionInfo, error) {
	u, err := url.Parse(c.config.ControllerURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/version"

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.config.Secret) != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.Secret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("controller unreachable at %s: %w", c.config.RedactedControllerURL(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /version returned HTTP %d", resp.StatusCode)
	}

	var info VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode /version response: %w", err)
	}

	return &info, nil
}

// ConnectWebSocket 建立符合 RFC 6455 规范的 WebSocket 连接 (支持 TLS/WSS 握手校验)
func (c *ControllerClient) ConnectWebSocket(ctx context.Context) (*websocket.Conn, error) {
	u, err := url.Parse(c.config.ControllerURL)
	if err != nil {
		return nil, err
	}

	scheme := "ws"
	if u.Scheme == "https" || u.Scheme == "wss" {
		scheme = "wss"
	}

	intervalParam := ""
	if c.config.ConnectionsInterval > 0 {
		intervalParam = fmt.Sprintf("?interval=%d", c.config.ConnectionsInterval)
	}

	wsURL := fmt.Sprintf("%s://%s/connections%s", scheme, u.Host, intervalParam)

	requestHeader := make(http.Header)
	if strings.TrimSpace(c.config.Secret) != "" {
		requestHeader.Set("Authorization", "Bearer "+c.config.Secret)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}

	conn, resp, err := dialer.DialContext(ctx, wsURL, requestHeader)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("WS dial failed with HTTP %d: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("WS dial failed: %w", err)
	}

	// 设定 32MB 安全帧上限
	conn.SetReadLimit(32 * 1024 * 1024)

	return conn, nil
}

// RunStreamLoop 运行 WebSocket 消费循环，带 Stream Idle Watchdog 与指数退避重连
func (c *ControllerClient) RunStreamLoop(
	ctx context.Context,
	q *queue.BoundedQueue[*types.IngestItem],
) error {
	backoff := time.Duration(c.config.InitialBackoffMs) * time.Millisecond
	maxBackoff := time.Duration(c.config.MaxBackoffMs) * time.Millisecond
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	if maxBackoff <= 0 {
		maxBackoff = 10 * time.Second
	}

	watchdogTimeout := 5 * time.Second
	if c.config.ConnectionsInterval > 0 {
		calcTimeout := time.Duration(c.config.ConnectionsInterval*8) * time.Millisecond
		if calcTimeout > watchdogTimeout {
			watchdogTimeout = calcTimeout
		}
	}

	var sessionFrameCount int

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// 1. Preflight 检查
		_, err := c.CheckVersion(ctx)
		if err != nil {
			c.notifyStatus("waiting")
			if !c.hasConnectedBefore.Load() {
				// 启动时尚未连上：仅记录 Health，不生成虚假 Gap
				_ = q.Push(ctx, &types.IngestItem{
					Kind:        types.ItemCollectorHealth,
					Timestamp:   time.Now(),
					HealthIssue: "controller_unavailable_before_first_coverage",
					Details: map[string]any{
						"controller": c.config.RedactedControllerURL(),
						"error":      err.Error(),
					},
				})
			}
			jitter := time.Duration(getRandomJitterMs(100)) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff + jitter):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// 2. 建立 WebSocket 连接
		conn, err := c.ConnectWebSocket(ctx)
		if err != nil {
			c.notifyStatus("waiting")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		c.hasConnectedBefore.Store(true)
		c.notifyStatus("connected")
		backoff = time.Duration(c.config.InitialBackoffMs) * time.Millisecond

		// 异步响应 Context 取消
		closeCh := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"), time.Now().Add(time.Second))
				_ = conn.Close()
			case <-closeCh:
			}
		}()

		// 3. 读取快照帧并推入有序队列
		for {
			// 设置 ReadDeadline 实现 Stream Idle 看门狗
			_ = conn.SetReadDeadline(time.Now().Add(watchdogTimeout))
			messageType, payloadBytes, err := conn.ReadMessage()
			if err != nil {
				c.notifyStatus("reconnecting")
				close(closeCh)
				_ = conn.Close()

				isWatchdogTimeout := false
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					isWatchdogTimeout = true
					_ = q.Push(ctx, &types.IngestItem{
						Kind:        types.ItemCollectorHealth,
						Timestamp:   time.Now(),
						HealthIssue: "stream_stalled_watchdog_timeout",
						Details: map[string]any{
							"watchdogTimeoutMs": watchdogTimeout.Milliseconds(),
						},
					})
				}

				if ctx.Err() != nil {
					// 正常关闭退出
					return nil
				}

				// 触发 GapOpened
				pushErr := q.Push(ctx, &types.IngestItem{
					Kind:      types.ItemGapOpened,
					Timestamp: time.Now(),
					Details: map[string]any{
						"isWatchdogTimeout": isWatchdogTimeout,
					},
				})
				if pushErr != nil && !errors.Is(pushErr, context.Canceled) {
					return pushErr
				}
				break
			}

			if messageType != websocket.TextMessage {
				continue
			}

			sessionFrameCount++

			var payload types.ConnectionSnapshotPayload
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				_ = q.Push(ctx, &types.IngestItem{
					Kind:        types.ItemCollectorHealth,
					Timestamp:   time.Now(),
					HealthIssue: "frame_json_decode_error",
					Details: map[string]any{
						"error": err.Error(),
					},
				})
				continue
			}

			frame := &types.ConnectionSnapshotFrame{
				ReceivedAt: time.Now().Format(time.RFC3339Nano),
				Frame:      payload,
			}

			item := &types.IngestItem{
				Kind:      types.ItemFrame,
				Timestamp: time.Now(),
				Frame:     frame,
			}

			if err := q.Push(ctx, item); err != nil {
				// context canceled
				break
			}

			// 验证模式下的故障重连注入
			if c.ValidationForceDisconnectAfterFrames > 0 && sessionFrameCount >= c.ValidationForceDisconnectAfterFrames {
				c.ValidationForceDisconnectAfterFrames = 0 // 仅触发一次
				nonceBytes := make([]byte, 8)
				rand.Read(nonceBytes)
				injectionID := "inj-" + hex.EncodeToString(nonceBytes)

				close(closeCh)
				_ = conn.Close()
				_ = q.Push(ctx, &types.IngestItem{
					Kind:      types.ItemGapOpened,
					Timestamp: time.Now(),
					Details: map[string]any{
						"injected":    true,
						"injectionId": injectionID,
						"reason":      "validation_forced_disconnect",
					},
				})
				break
			}
		}
	}
}

func getRandomJitterMs(max int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return n.Int64()
}
