package client

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	config     *config.Config
	httpClient *http.Client
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

// ReadWebSocketFrames 从底层 TCP 连接读取 WebSocket Text 帧 (RFC 6455)
func ReadWebSocketFrames(r *bufio.Reader) ([]byte, error) {
	for {
		header, err := r.ReadByte()
		if err != nil {
			return nil, err
		}

		opcode := header & 0x0F
		// Opcode 1: Text, Opcode 8: Close, Opcode 9: Ping, Opcode 10: Pong
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

		if opcode == 0x01 { // Text Frame
			return payload, nil
		}
		// 忽略 ping/pong 等控制帧，继续读下一帧
	}
}

// ConnectWebSocket 建立与 /connections 的 WebSocket 流
func (c *ControllerClient) ConnectWebSocket(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(c.config.ControllerURL)
	if err != nil {
		return nil, nil, err
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, err
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

// RunStreamLoop 执行 WebSocket 监听循环与指数退避重连
func (c *ControllerClient) RunStreamLoop(
	ctx context.Context,
	q *queue.BoundedQueue[*types.ConnectionSnapshotFrame],
	onGapOpened func(time.Time),
) error {
	backoff := time.Duration(c.config.InitialBackoffMs) * time.Millisecond
	maxBackoff := time.Duration(c.config.MaxBackoffMs) * time.Millisecond
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	if maxBackoff <= 0 {
		maxBackoff = 10 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// 1. Preflight 检查
		_, err := c.CheckVersion(ctx)
		if err != nil {
			if onGapOpened != nil {
				onGapOpened(time.Now())
			}
			// 增加 Jitter
			jitter := time.Duration(getRandomJitterMs(100)) * time.Millisecond
			sleepDur := backoff + jitter
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(sleepDur):
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
			if onGapOpened != nil {
				onGapOpened(time.Now())
			}
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

		// 成功建立连接，重置退避
		backoff = time.Duration(c.config.InitialBackoffMs) * time.Millisecond

		// 3. 读取快照帧
		for {
			select {
			case <-ctx.Done():
				conn.Close()
				return nil
			default:
			}

			payloadBytes, err := ReadWebSocketFrames(reader)
			if err != nil {
				conn.Close()
				if onGapOpened != nil {
					onGapOpened(time.Now())
				}
				break // 连接中断，进入重连循环
			}

			var payload types.ConnectionSnapshotPayload
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				continue
			}

			frame := &types.ConnectionSnapshotFrame{
				ReceivedAt: time.Now().Format(time.RFC3339Nano),
				Frame:      payload,
			}

			// 放入有界队列
			if err := q.Push(frame); err != nil {
				// 队列过载告警
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
