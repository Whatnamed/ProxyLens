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
	token := flag.String("token", "", "High-entropy session token for Bearer authentication")
	flag.Parse()

	if *dbPath == "" || *token == "" {
		fmt.Fprintf(os.Stderr, "Usage: proxylens-query-api --db <path> --token <token> [--listen 127.0.0.1:0]\n")
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

	// 2. 以严格只读模式打开 SQLite DB
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
		Token:      *token,
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
		// 监听父进程管道是否断开 (Tauri 退出时 stdin 会关闭)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_ = server.Stop(shutdownCtx)
}
