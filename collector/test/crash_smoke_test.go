package test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

func TestSubprocessCrashKillAndReopenSmoke(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "proxylens-crash-smoke-*")
	if err != nil {
		t.Fatalf("Failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "crash-test.db")

	collectorBin, err := filepath.Abs("../collector.exe")
	if err != nil {
		t.Fatalf("Failed to get abs path of collector binary: %v", err)
	}
	if _, err := os.Stat(collectorBin); os.IsNotExist(err) {
		// 备用位置
		collectorBin, _ = filepath.Abs("./collector.exe")
	}

	// 1. 启动子进程 1
	cmd1 := exec.Command(collectorBin, "run", "--controller", "http://127.0.0.1:9090", "--connections-interval", "250", "--db", dbPath)
	cmd1.Stdout = os.Stdout
	cmd1.Stderr = os.Stderr

	if err := cmd1.Start(); err != nil {
		t.Fatalf("Failed to start collector subprocess 1: %v", err)
	}

	// 等待 2 秒让其初始化 session 与落盘
	time.Sleep(2 * time.Second)

	// 2. 强行 Kill 子进程 1 (模拟崩溃/SIGKILL)
	if err := cmd1.Process.Kill(); err != nil {
		t.Fatalf("Failed to kill subprocess 1: %v", err)
	}
	_ = cmd1.Wait()

	// 等待 1 秒
	time.Sleep(1 * time.Second)

	// 3. 启动子进程 2 恢复
	cmd2 := exec.Command(collectorBin, "run", "--controller", "http://127.0.0.1:9090", "--connections-interval", "250", "--db", dbPath)
	stdinPipe, err := cmd2.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to get stdin pipe: %v", err)
	}
	cmd2.Stdout = os.Stdout
	cmd2.Stderr = os.Stderr

	if err := cmd2.Start(); err != nil {
		t.Fatalf("Failed to start collector subprocess 2: %v", err)
	}

	time.Sleep(2 * time.Second)

	// 优雅停止子进程 2
	_, _ = stdinPipe.Write([]byte("STOP\n"))
	_ = cmd2.Wait()

	// 4. 打开数据库检验崩溃恢复和 Gap 生成
	ctx := context.Background()
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen database after subprocess crash: %v", err)
	}
	defer db.Close()

	qs := storage.NewQueryService(db)
	gaps, err := qs.ListMonitoringGaps(ctx, nil, nil)
	if err != nil {
		t.Fatalf("Failed to query monitoring gaps: %v", err)
	}

	var foundCrashGap bool
	for _, g := range gaps {
		if g.Source == "collector_session_boundary" && g.Reason == "collector_unclean_shutdown_or_process_termination" {
			foundCrashGap = true
		}
	}

	if !foundCrashGap {
		t.Errorf("Subprocess crash recovery failed: expected offline gap with reason 'collector_unclean_shutdown_or_process_termination', got %+v", gaps)
	}
}
