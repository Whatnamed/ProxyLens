package client

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/queue"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// VersionInfo 映射 GET /version 返回
type VersionInfo struct {
	Meta    bool   `json:"meta"`
	Version string `json:"version"`
}

// ControllerClient 提供只读的 Mihomo Controller API 访问与 WebSocket 监听
type ControllerClient struct {
	config                              *config.Config
	httpClient                          *http.Client
	hasConnectedBefore                  atomic.Bool
	ValidationForceDisconnectAfterFrames int
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

// ReadWebSocketMessage 从连接读取完整的 WebSocket Text 消息 (支持 masked client Pong, 分片帧, 超时与 Close)
func ReadWebSocketMessage(conn net.Conn, r *bufio.Reader) ([]byte, error) {
	var assembledPayload []byte

	for {
		header, err := r.ReadByte()
		if err != nil {
			return nil, err
		}

		fin := (header & 0x80) != 0
		opcode := header & 0x0F

		// Opcode 8: Close
		if opcode == 0x08 {
			return nil, io.EOF
		}

		b2, err := r.ReadByte()
		if err != nil {
			return nil, err
		}

		isMasked := (b2 & 0x80) != 0
		length := int64(b2 & 0x7F)

		if length == 126 {
			var l uint16
			b := make([]byte, 2)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil, err
			}
			l = uint16(b[0])<<8 | uint16(b[1])
			length = int64(l)
		} else if length == 127 {
			b := make([]byte, 8)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil, err
			}
			length = 0
			for i := 0; i < 8; i++ {
				length = (length << 8) | int64(b[i])
			}
		}

		var maskKey []byte
		if isMasked {
			maskKey = make([]byte, 4)
			if _, err := io.ReadFull(r, maskKey); err != nil {
				return nil, err
			}
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}

		if isMasked {
			for i := int64(0); i < length; i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		// Opcode 9: Ping -> 自动回复 Masked Pong (Opcode 10) 符合 RFC 6455
		if opcode == 0x09 {
			pongMask := make([]byte, 4)
			rand.Read(pongMask)
			maskedPayload := make([]byte, len(payload))
			for i := range payload {
				maskedPayload[i] = payload[i] ^ pongMask[i%4]
			}
			pongHeader := []byte{0x8A, byte(0x80 | len(payload))}
			pongHeader = append(pongHeader, pongMask...)
			pongHeader = append(pongHeader, maskedPayload...)
			_, _ = conn.Write(pongHeader)
			continue
		}

		// Opcode 10: Pong (忽略)
		if opcode == 0x0A {
			continue
		}

		// Opcode 1: Text, Opcode 0: Continuation
		if opcode == 0x01 || opcode == 0x00 {
			assembledPayload = append(assembledPayload, payload...)
			if fin {
				return assembledPayload, nil
			}
			continue
		}
	}
}

// ConnectWebSocket 建立与 /connections 的 WebSocket 流 (支持 TLS/WSS)
func (c *ControllerClient) ConnectWebSocket(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(c.config.ControllerURL)
	if err != nil {
		return nil, nil, err
	}

	host := u.Host
	isTLS := u.Scheme == "https" || u.Scheme == "wss"
	if !strings.Contains(host, ":") {
		if isTLS {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	var dialer net.Dialer
	rawConn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, err
	}

	var conn net.Conn = rawConn
	if isTLS {
		serverName := u.Hostname()
		tlsConn := tls.Client(rawConn, &tls.Config{
			ServerName: serverName,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			rawConn.Close()
			return nil, nil, fmt.Errorf("TLS handshake failed: %w", err)
		}
		conn = tlsConn
	}

	// 生成随机 Sec-WebSocket-Key
	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	intervalParam := ""
	if c.config.ConnectionsInterval > 0 {
		intervalParam = fmt.Sprintf("?interval=%d", c.config.ConnectionsInterval)
	}

	path := fmt.Sprintf("/connections%s", intervalParam)
	authHeader := ""
	if strings.TrimSpace(c.config.Secret) != "" {
		authHeader = fmt.Sprintf("Authorization: Bearer %s\r\n", c.config.Secret)
	}

	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"%s\r\n",
		path, u.Host, wsKey, authHeader,
	)

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, nil, err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to read WS upgrade response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, fmt.Errorf("WS upgrade rejected with HTTP %d", resp.StatusCode)
	}

	return conn, reader, nil
}

// RunStreamLoop 执行 WebSocket 监听循环与看门狗重连
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
		conn, reader, err := c.ConnectWebSocket(ctx)
		if err != nil {
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
		backoff = time.Duration(c.config.InitialBackoffMs) * time.Millisecond

		doneCh := make(chan struct{})
		lastFrameTime := atomic.Int64{}
		lastFrameTime.Store(time.Now().UnixNano())

		// 启动 Stream Idle 看门狗协程
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					conn.Close()
					return
				case <-doneCh:
					return
				case <-ticker.C:
					lastTs := time.Unix(0, lastFrameTime.Load())
					if time.Since(lastTs) > watchdogTimeout {
						// 看门狗超时：half-open 连接判定，强制关闭
						_ = q.Push(ctx, &types.IngestItem{
							Kind:        types.ItemCollectorHealth,
							Timestamp:   time.Now(),
							HealthIssue: "stream_stalled_watchdog_timeout",
							Details: map[string]any{
								"watchdogTimeoutMs": watchdogTimeout.Milliseconds(),
							},
						})
						conn.Close()
						return
					}
				}
			}
		}()

		// 3. 读取快照帧并推入有序队列
		for {
			payloadBytes, err := ReadWebSocketMessage(conn, reader)
			if err != nil {
				close(doneCh)
				conn.Close()

				gapOpenedTime := time.Now()
				_ = q.Push(ctx, &types.IngestItem{
					Kind:      types.ItemGapOpened,
					Timestamp: gapOpenedTime,
				})

				break
			}

			lastFrameTime.Store(time.Now().UnixNano())
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

			// 保证入队，天然 Backpressure
			if err := q.Push(ctx, item); err != nil {
				// context canceled
				break
			}

			// 支持验证模式下的故障重连注入
			if c.ValidationForceDisconnectAfterFrames > 0 && sessionFrameCount >= c.ValidationForceDisconnectAfterFrames {
				c.ValidationForceDisconnectAfterFrames = 0 // 仅触发一次
				close(doneCh)
				conn.Close()
				_ = q.Push(ctx, &types.IngestItem{
					Kind:      types.ItemGapOpened,
					Timestamp: time.Now(),
					Details: map[string]any{
						"injected": true,
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
