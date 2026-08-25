package test

import (
	"context"
	"database/sql"
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

	// 等待 2.5 秒让子进程写入初始化 session 与落盘事件
	time.Sleep(2500 * time.Millisecond)

	// 2. 强行 Kill 子进程 1 (模拟崩溃/SIGKILL)
	if err := cmd1.Process.Kill(); err != nil {
		t.Fatalf("Failed to kill subprocess 1: %v", err)
	}
	_ = cmd1.Wait()

	// 3. 在启动子进程 2 前，通过只读连接确认在崩溃前至少已有一条 committed journal 记录
	readDB, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("Failed to open read db after kill: %v", err)
	}
	var preCrashJournalCount int
	var preCrashEventID string
	err = readDB.QueryRow("SELECT COUNT(*), COALESCE(MAX(event_id), '') FROM event_journal").Scan(&preCrashJournalCount, &preCrashEventID)
	if err != nil {
		_ = readDB.Close()
		t.Fatalf("Failed to query pre-crash journal: %v", err)
	}
	_ = readDB.Close()

	if preCrashJournalCount == 0 || preCrashEventID == "" {
		t.Fatalf("Expected at least 1 committed journal event before crash, got %d", preCrashJournalCount)
	}

	// 等待 1 秒
	time.Sleep(1 * time.Second)

	// 4. 启动子进程 2 恢复
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

	// 5. 打开数据库检验崩溃恢复、Gap 生成以及 preCrashEventID 依然完整保留
	ctx := context.Background()
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen database after subprocess crash: %v", err)
	}
	defer db.Close()

	// 断言崩溃前已提交的 event 依然完整保留
	var postReopenEventCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM event_journal WHERE event_id = ?", preCrashEventID).Scan(&postReopenEventCount)
	if err != nil || postReopenEventCount != 1 {
		t.Errorf("Committed event %s before crash missing after reopen! count=%d, err=%v", preCrashEventID, postReopenEventCount, err)
	}

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
