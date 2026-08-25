package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/api"
	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

type ReadySignal struct {
	Type       string `json:"type"`
	APIVersion string `json:"apiVersion"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
}

func main() {
	dbPath := flag.String("db", "", "Absolute path to ProxyLens SQLite database")
	listenAddr := flag.String("listen", "127.0.0.1:0", "Listen address (must be loopback, e.g. 127.0.0.1:0)")
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: proxylens-query-api --db <path> [--listen 127.0.0.1:0]\n(Note: session token must be provided via stdin first line)\n")
		os.Exit(1)
	}

	stdinScanner := bufio.NewScanner(os.Stdin)
	var sessionToken string

	// 安全通道: 严格且仅从 stdin 第一行读取 token，杜绝 argv 泄露给系统进程列表
	if stdinScanner.Scan() {
		sessionToken = strings.TrimSpace(stdinScanner.Text())
	}
	if sessionToken == "" {
		fmt.Fprintf(os.Stderr, "[FATAL SECURITY ERROR] No session token provided via stdin pipe\n")
		os.Exit(1)
	}

	// 1. 安全检查: 必须绑定 Loopback
	host, _, err := net.SplitHostPort(*listenAddr)
	if err != nil {
		host = *listenAddr
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		fmt.Fprintf(os.Stderr, "[FATAL SECURITY ERROR] proxylens-query-api only allows binding to loopback (127.0.0.1 / localhost), got: %s\n", *listenAddr)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. 以严格只读模式打开 SQLite DB (mode=ro + query_only=ON)
	db, err := storage.OpenReadOnlyDB(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to open read-only database at %s: %v\n", *dbPath, err)
		os.Exit(1)
	}
	defer db.Close()

	// 3. 初始化并启动 Local Query API Server
	server, err := api.NewServer(api.ServerConfig{
		DB:         db,
		DBPath:     *dbPath,
		ListenAddr: *listenAddr,
		Token:      sessionToken,
		AppVersion: "0.7.0-phase3a",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to create server: %v\n", err)
		os.Exit(1)
	}

	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to start server: %v\n", err)
		os.Exit(1)
	}

	// 4. 获取分配的随机端口并输出就绪 JSON
	port := server.Port()
	readyPayload := ReadySignal{
		Type:       "proxylens-query-api-ready",
		APIVersion: "v1",
		Host:       "127.0.0.1",
		Port:       port,
	}
	readyBytes, _ := json.Marshal(readyPayload)
	// 严格按规范只在 stdout 打印单行就绪信号
	fmt.Println(string(readyBytes))

	// 5. 监听优雅停止信号与父进程 stdin 关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		// 监听父进程管道是否发送 STOP 或关闭 (Tauri 退出时 stdin 会关闭)
		for stdinScanner.Scan() {
			text := strings.TrimSpace(stdinScanner.Text())
			if text == "STOP" || text == "QUIT" {
				break
			}
		}
		cancel()
	}()

	select {
	case <-sigCh:
	case <-ctx.Done():
	}

	// 优雅关闭 API Server (500ms 超时)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}
