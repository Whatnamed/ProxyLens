package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func createAccountingTestDB(t *testing.T) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "proxylens-accounting-test-*")
	if err != nil {
		t.Fatalf("Failed to create tmp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "accounting_test.db")
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	return dbPath, cleanup
}

func TestStorageFullMigrationChainV1ToV5(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	// 1. 手动直接执行 001_initial.sql 创建 v1 数据库
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open raw sqlite db: %v", err)
	}

	v1Content, err := migrationFS.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatalf("Failed to read v1 migration: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, string(v1Content)); err != nil {
		t.Fatalf("Failed to execute v1 migration: %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, "INSERT INTO schema_migrations (version, name, applied_at) VALUES (1, '001_initial.sql', '2026-08-25T00:00:00Z')"); err != nil {
		t.Fatalf("Failed to record v1 migration: %v", err)
	}
	_ = rawDB.Close()

	// 2. 通过 OpenDB 连续升级至最高版本 (v5)
	upgradedDB, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed to run migrations: %v", err)
	}
	defer upgradedDB.Close()

	var maxVer int
	if err := upgradedDB.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&maxVer); err != nil {
		t.Fatalf("Query max version failed: %v", err)
	}
	if maxVer != 5 {
		t.Fatalf("Expected database schema to be upgraded to version 5, got %d", maxVer)
	}
}

func TestObservationLifecycleSemantics(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-lifecycle", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open sink failed: %v", err)
	}

	t0 := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)

	// 1. 创建两条连接：conn-disappear 与 conn-epoch
	ev1 := &types.CollectorEvent{
		EventID: "e1", SessionID: "sess-lifecycle", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "conn-disappear",
		Metadata: types.RawMetadata{Process: "curl.exe"},
	}
	ev2 := &types.CollectorEvent{
		EventID: "e2", SessionID: "sess-lifecycle", EpochID: 1, FrameSequence: 1, EventSequence: 2,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "conn-epoch",
		Metadata: types.RawMetadata{Process: "git.exe"},
	}
	if err := sink.Emit(ev1); err != nil { t.Fatalf("Emit e1 failed: %v", err) }
	if err := sink.Emit(ev2); err != nil { t.Fatalf("Emit e2 failed: %v", err) }

	// 2. conn-disappear 发出 Disappeared 事件
	evDis := &types.CollectorEvent{
		EventID: "e3", SessionID: "sess-lifecycle", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDisappeared, Timestamp: t0.Add(5 * time.Second), ConnectionID: "conn-disappear",
	}
	if err := sink.Emit(evDis); err != nil { t.Fatalf("Emit disappeared failed: %v", err) }

	// 3. 发出 CounterEpochBreak 事件，结束 Epoch 1 下剩余连接
	evEpoch := &types.CollectorEvent{
		EventID: "e4", SessionID: "sess-lifecycle", EpochID: 1, FrameSequence: 3, EventSequence: 1,
		Type: types.EventCounterEpochBreak, Timestamp: t0.Add(10 * time.Second),
	}
	if err := sink.Emit(evEpoch); err != nil { t.Fatalf("Emit epoch break failed: %v", err) }

	qs := NewQueryService(sink.db)
	c1, err := qs.GetConnection(ctx, "sess-lifecycle", 1, "conn-disappear")
	if err != nil { t.Fatalf("Get c1 failed: %v", err) }
	if c1.ObservationActive || c1.ObservationEndReason != "disappeared_from_snapshot" || c1.ObservationEndedAt == nil {
		t.Errorf("c1 observation lifecycle mismatch: %+v", c1)
	}

	c2, err := qs.GetConnection(ctx, "sess-lifecycle", 1, "conn-epoch")
	if err != nil { t.Fatalf("Get c2 failed: %v", err) }
	if c2.ObservationActive || c2.ObservationEndReason != "epoch_boundary" || c2.ObservationEndedAt == nil {
		t.Errorf("c2 observation lifecycle mismatch: %+v", c2)
	}

	// 4. 执行 RebuildProjections，校验 lifecycle 一致性
	if err := RebuildProjections(ctx, sink.db); err != nil {
		t.Fatalf("RebuildProjections failed: %v", err)
	}
	c1After, _ := qs.GetConnection(ctx, "sess-lifecycle", 1, "conn-disappear")
	if c1After.ObservationEndReason != "disappeared_from_snapshot" || c1After.ObservationActive {
		t.Errorf("c1 mismatch after rebuild: %+v", c1After)
	}
	c2After, _ := qs.GetConnection(ctx, "sess-lifecycle", 1, "conn-epoch")
	if c2After.ObservationEndReason != "epoch_boundary" || c2After.ObservationActive {
		t.Errorf("c2 mismatch after rebuild: %+v", c2After)
	}
	_ = sink.Close()
}

func TestConservativeRelayReconciliation(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-relay-test", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open sink failed: %v", err)
	}

	t0 := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	emitCheck := func(ev *types.CollectorEvent) {
		if err := sink.Emit(ev); err != nil {
			t.Fatalf("Emit event %s (%s) failed: %v", ev.EventID, ev.Type, err)
		}
	}

	// Frame 1: 所有连接的 Bootstrap 初始快照 (Frame 1, Seq 1~6)
	// 1. Logical 连接 (Chrome -> PROXY -> Node-HK -> Google)
	emitCheck(&types.CollectorEvent{
		EventID: "r-boot-log", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-log-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		ObservedUploadCounter: 100, ObservedDownloadCounter: 200,
		Metadata: types.RawMetadata{Process: "chrome.exe", Host: "google.com", Network: "tcp"},
		Chains: []string{"Node-HK", "Proxy-Group"},
	})
	// 2. Candidate 底层中继连接 (Process为空, 目标为中继 IP, 结构与出站节点一致)
	emitCheck(&types.CollectorEvent{
		EventID: "r-boot-cand", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 2,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-cand-1",
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
		ObservedUploadCounter: 100, ObservedDownloadCounter: 200,
		Metadata: types.RawMetadata{Process: "", DestinationIP: "1.2.3.4", Network: "tcp"},
		Chains: []string{"Node-HK"},
	})
	// 3. DIRECT 独立连接 (curl -> DIRECT)
	emitCheck(&types.CollectorEvent{
		EventID: "d-boot", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 3,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-direct-1",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		ObservedUploadCounter: 10, ObservedDownloadCounter: 10,
		Metadata: types.RawMetadata{Process: "curl.exe", Host: "ntp.aliyun.com", Network: "udp"},
		Chains: []string{"DIRECT"},
	})
	// 4. Ambiguous candidate
	emitCheck(&types.CollectorEvent{
		EventID: "amb-cand-boot", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 4,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-amb-cand",
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
		Chains: []string{"Node-US"},
	})
	// 5. Ambiguous logical 1
	emitCheck(&types.CollectorEvent{
		EventID: "amb-log1-boot", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 5,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-amb-log1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "app1.exe"}, Chains: []string{"Node-US"},
	})
	// 6. Ambiguous logical 2
	emitCheck(&types.CollectorEvent{
		EventID: "amb-log2-boot", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 6,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-amb-log2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "app2.exe"}, Chains: []string{"Node-US"},
	})

	// Frame 2: 所有连接的增量 Delta (Frame 2, Seq 1~6)
	emitCheck(&types.CollectorEvent{
		EventID: "r-delta-log", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-log-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		DeltaUpload: 1000, DeltaDownload: 2000, ObservedUploadCounter: 1100, ObservedDownloadCounter: 2200,
		MonitoredCumulativeUpload: 1000, MonitoredCumulativeDownload: 2000,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "r-delta-cand", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 2,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-cand-1",
		Route: types.RouteProxy, AttributionClass: types.ClassConfirmedRelayDuplicate,
		DeltaUpload: 1000, DeltaDownload: 2000, ObservedUploadCounter: 1100, ObservedDownloadCounter: 2200,
		MonitoredCumulativeUpload: 1000, MonitoredCumulativeDownload: 2000,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "d-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 3,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-direct-1",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		DeltaUpload: 500, DeltaDownload: 500, ObservedUploadCounter: 510, ObservedDownloadCounter: 510,
		MonitoredCumulativeUpload: 500, MonitoredCumulativeDownload: 500,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "amb-cand-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 4,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-amb-cand",
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
		DeltaUpload: 300, DeltaDownload: 400, MonitoredCumulativeUpload: 300, MonitoredCumulativeDownload: 400,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "amb-log1-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 5,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-amb-log1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		DeltaUpload: 300, DeltaDownload: 400, MonitoredCumulativeUpload: 300, MonitoredCumulativeDownload: 400,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "amb-log2-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 6,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-amb-log2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		DeltaUpload: 300, DeltaDownload: 400, MonitoredCumulativeUpload: 300, MonitoredCumulativeDownload: 400,
	})

	_ = sink.Close()

	// 运行 RebuildAccounting
	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	run, err := RebuildAccounting(ctx, db, "test relay run")
	if err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}
	if run.Status != AccountingRunCompleted {
		t.Fatalf("Expected completed status, got %s", run.Status)
	}

	analyticsSvc := NewAnalyticsService(db)
	summary, err := analyticsSvc.GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}

	// 验证：
	// Raw PROXY Up: c-log-1(1000) + c-cand-1(1000) + c-amb-cand(300) + c-amb-log1(300) + c-amb-log2(300) = 2900
	// Accounted PROXY Up: c-log-1(1000) + c-cand-1(0, 去重) + c-amb-cand(300, 歧义不扣) + c-amb-log1(300) + c-amb-log2(300) = 1900
	// DIRECT Up: 500 (独立统计)
	if summary.ProxyUpload != 1900 || summary.ProxyDownload != 3200 {
		t.Errorf("Proxy upload/download mismatch: Up=%d (expected 1900), Down=%d (expected 3200)", summary.ProxyUpload, summary.ProxyDownload)
	}
	if summary.DirectUpload != 500 || summary.DirectDownload != 500 {
		t.Errorf("Direct upload/download mismatch: Up=%d, Down=%d", summary.DirectUpload, summary.DirectDownload)
	}
	if summary.AmbiguousRelayUpload != 300 || summary.AmbiguousRelayDownload != 400 {
		t.Errorf("Ambiguous relay mismatch: Up=%d, Down=%d", summary.AmbiguousRelayUpload, summary.AmbiguousRelayDownload)
	}

	// 验证 10 次确定性 Rebuild (E8.4)
	for i := 0; i < 10; i++ {
		reRun, err := RebuildAccounting(ctx, db, fmt.Sprintf("determinism %d", i))
		if err != nil {
			t.Fatalf("Rebuild iteration %d failed: %v", i, err)
		}
		var accSum int64
		_ = db.QueryRowContext(ctx, "SELECT SUM(accounted_upload + accounted_download) FROM accounted_traffic WHERE run_id = ?", reRun.RunID).Scan(&accSum)
		expectedSum := (1900 + 3200) + (500 + 500)
		if accSum != int64(expectedSum) {
			t.Errorf("Determinism checksum mismatch at iteration %d: got %d, expected %d", i, accSum, expectedSum)
		}
	}
}

func TestHourlyAllocationByteConservation(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-alloc-test", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 11, 30, 0, 0, time.UTC)
	t1 := time.Date(2026, 8, 25, 12, 30, 0, 0, time.UTC) // 跨 11:00 和 12:00 两个整小时 bucket

	_ = sink.Emit(&types.CollectorEvent{
		EventID: "b1", SessionID: "sess-alloc-test", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-cross",
		Metadata: types.RawMetadata{Process: "downloader.exe"},
	})
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "d1", SessionID: "sess-alloc-test", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t1, ConnectionID: "c-cross",
		AttributionInterval: []string{t0.Format(time.RFC3339Nano), t1.Format(time.RFC3339Nano)},
		Precision: "interval_only",
		DeltaUpload: 10000000001, DeltaDownload: 1, // 大数字与 1 字节余数测试
		MonitoredCumulativeUpload: 10000000001, MonitoredCumulativeDownload: 1,
	})
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	run, err := RebuildAccounting(ctx, db, "test alloc")
	if err != nil { t.Fatalf("RebuildAccounting failed: %v", err) }

	// 查询 hourly aggregates 中 total 维度两个小时的总和
	var sumUp, sumDown int64
	err = db.QueryRowContext(ctx, `
		SELECT SUM(upload_bytes), SUM(download_bytes)
		FROM usage_hourly_dimensions
		WHERE run_id = ? AND dimension_type = 'total';
	`, run.RunID).Scan(&sumUp, &sumDown)
	if err != nil {
		t.Fatalf("Query hourly sum failed: %v", err)
	}

	// 必须绝对字节守恒 (E8.5)
	if sumUp != 10000000001 || sumDown != 1 {
		t.Fatalf("Byte conservation broken! Expected (10000000001, 1), got (%d, %d)", sumUp, sumDown)
	}
}

func TestMonitoringCoverageIntervalUnion(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	// 1. 初始化 DB 并创建 Session
	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	nowStr := "2026-08-25T10:00:00Z"
	_, _ = db.Exec(`
		INSERT INTO collector_sessions (session_id, started_at, status, created_at, updated_at)
		VALUES ('sess-cov', '2026-08-25T10:00:00Z', 'closed_clean', ?, ?);
	`, nowStr, nowStr)

	// 2. 插入两个重叠的 Gap (Gap 1: 10:00~10:10, Gap 2: 10:05~10:15)
	_, _ = db.Exec(`
		INSERT INTO monitoring_gaps (gap_id, source, session_id, started_at, ended_at, duration_ms, reason, precision, created_at)
		VALUES
			('g1', 'controller_stream', 'sess-cov', '2026-08-25T10:00:00Z', '2026-08-25T10:10:00Z', 600000, 'reconnect', 'interval', ?),
			('g2', 'collector_session_boundary', 'sess-cov', '2026-08-25T10:05:00Z', '2026-08-25T10:15:00Z', 600000, 'offline', 'interval', ?);
	`, nowStr, nowStr)

	svc := NewAnalyticsService(db)

	// 查询区间 10:00 ~ 11:00 (1 小时 = 3,600,000 ms)
	qStart, _ := time.Parse(time.RFC3339, "2026-08-25T10:00:00Z")
	qEnd, _ := time.Parse(time.RFC3339, "2026-08-25T11:00:00Z")

	cov, err := svc.GetCoverage(ctx, &qStart, &qEnd)
	if err != nil {
		t.Fatalf("GetCoverage failed: %v", err)
	}

	// 两个重叠 Gap 并集后应为 10:00 ~ 10:15 (15 分钟 = 900,000 ms)
	if len(cov.MergedGaps) != 1 {
		t.Errorf("Expected 1 merged gap from overlapping intervals, got %d", len(cov.MergedGaps))
	}
	if cov.UncoveredDurationMs != 900000 {
		t.Errorf("Expected 900,000 ms uncovered (15 mins), got %d ms", cov.UncoveredDurationMs)
	}
	if cov.CoveredDurationMs != 2700000 {
		t.Errorf("Expected 2,700,000 ms covered (45 mins), got %d ms", cov.CoveredDurationMs)
	}
	if cov.CoverageRatio == nil || *cov.CoverageRatio != 0.75 {
		t.Errorf("Expected 0.75 coverage ratio (75%%), got %v", cov.CoverageRatio)
	}

	// 查询完全早于 known scope 的时间 (08:00 ~ 09:00)
	earlyStart, _ := time.Parse(time.RFC3339, "2026-08-25T08:00:00Z")
	earlyEnd, _ := time.Parse(time.RFC3339, "2026-08-25T09:00:00Z")
	earlyCov, err := svc.GetCoverage(ctx, &earlyStart, &earlyEnd)
	if err != nil {
		t.Fatalf("GetCoverage early failed: %v", err)
	}
	if earlyCov.CoverageRatio != nil {
		t.Errorf("Expected nil coverage ratio for outside known scope window, got %v", earlyCov.CoverageRatio)
	}
}

func TestAnalyticsServiceTopQueries(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-analytics", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC)

	// Frame 1: Bootstrap 初始快照 (Frame 1, Seq 1~2)
	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-boot-1", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "git.exe", Host: "github.com", Network: "tcp"},
		Chains: []string{"US-Proxy-1", "DefaultGroup"},
	}); err != nil { t.Fatalf("Emit a-boot-1 failed: %v", err) }

	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-boot-2", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 1, EventSequence: 2,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "curl.exe", Host: "api.anthropic.com", Network: "tcp"},
		Chains: []string{"JP-Proxy-2", "DefaultGroup"},
	}); err != nil { t.Fatalf("Emit a-boot-2 failed: %v", err) }

	// Frame 2: Delta 流量增量 (Frame 2, Seq 1~2)
	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-delta-1", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		DeltaUpload: 2000, DeltaDownload: 8000,
		MonitoredCumulativeUpload: 2000, MonitoredCumulativeDownload: 8000,
	}); err != nil { t.Fatalf("Emit a-delta-1 failed: %v", err) }

	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-delta-2", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 2, EventSequence: 2,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		DeltaUpload: 500, DeltaDownload: 1500,
		MonitoredCumulativeUpload: 500, MonitoredCumulativeDownload: 1500,
	}); err != nil { t.Fatalf("Emit a-delta-2 failed: %v", err) }
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	if _, err := RebuildAccounting(ctx, db, "analytics run"); err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}

	svc := NewAnalyticsService(db)

	// 1. Top Processes (git.exe 10000 B > curl.exe 2000 B)
	topProcs, err := svc.GetTopProcesses(ctx, AnalyticsFilter{Limit: 10})
	if err != nil || len(topProcs) < 2 {
		t.Fatalf("GetTopProcesses failed: %v, len=%d", err, len(topProcs))
	}
	if topProcs[0].Key != "git.exe" || topProcs[0].TotalBytes != 10000 {
		t.Errorf("Top process #1 mismatch: got %+v", topProcs[0])
	}

	// 2. Top Hosts (github.com > api.anthropic.com)
	topHosts, err := svc.GetTopHosts(ctx, AnalyticsFilter{Limit: 10})
	if err != nil || len(topHosts) < 2 {
		t.Fatalf("GetTopHosts failed: %v", err)
	}
	if topHosts[0].Key != "github.com" {
		t.Errorf("Top host #1 mismatch: got %+v", topHosts[0])
	}

	// 3. Top Outbound Proxies (US-Proxy-1 > JP-Proxy-2, derived from chains[0])
	topProxies, err := svc.GetTopFinalProxies(ctx, AnalyticsFilter{Limit: 10})
	if err != nil || len(topProxies) < 2 {
		t.Fatalf("GetTopFinalProxies failed: %v", err)
	}
	if topProxies[0].Key != "US-Proxy-1" {
		t.Errorf("Top proxy #1 mismatch: got %+v", topProxies[0])
	}

	// 4. Protocol Breakdown (tcp 12000 B)
	proto, err := svc.GetProtocolBreakdown(ctx, AnalyticsFilter{})
	if err != nil || len(proto) == 0 {
		t.Fatalf("GetProtocolBreakdown failed: %v", err)
	}
	if proto[0].Key != "tcp" || proto[0].TotalBytes != 12000 {
		t.Errorf("Protocol breakdown mismatch: got %+v", proto[0])
	}
}

func TestAccountingRebuildSanity10k(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-10k", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 15, 0, 0, 0, time.UTC)

	// 构造 10,000 条合成流量事件 (5,000 Bootstrap + 5,000 Delta)
	const count = 5000
	for i := 0; i < count; i++ {
		connID := fmt.Sprintf("c-10k-%d", i)
		_ = sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("b-%d", i), SessionID: "sess-10k", EpochID: 1, FrameSequence: 1, EventSequence: int64(i + 1),
			Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: connID,
			Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: fmt.Sprintf("proc-%d.exe", i%20), Host: fmt.Sprintf("host-%d.com", i%50)},
			Chains: []string{fmt.Sprintf("Node-%d", i%10)},
		})
	}
	for i := 0; i < count; i++ {
		connID := fmt.Sprintf("c-10k-%d", i)
		_ = sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("d-%d", i), SessionID: "sess-10k", EpochID: 1, FrameSequence: 2, EventSequence: int64(i + 1),
			Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: connID,
			Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
			DeltaUpload: 100, DeltaDownload: 200,
			MonitoredCumulativeUpload: 100, MonitoredCumulativeDownload: 200,
		})
	}
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	start := time.Now()
	run, err := RebuildAccounting(ctx, db, "10k sanity run")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RebuildAccounting 10k failed: %v", err)
	}
	if run.SourceJournalEventCount != int64(count*2) {
		t.Errorf("Expected %d source events, got %d", count*2, run.SourceJournalEventCount)
	}

	t.Logf("RebuildAccounting 10k events completed in %v (status: %s)", elapsed, run.Status)
}
