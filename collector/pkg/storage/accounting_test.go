package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/rand"
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

func TestIntervalAllocatorAdditiveInvariantAndSafety(t *testing.T) {
	t0 := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Hour)

	// 1. 10GB / 1h 两半测试 (应各约 5GB / 5GB，不得为负)
	const tenGB int64 = 10 * 1024 * 1024 * 1024
	alloc10G := NewIntervalAllocator(t0, t1, tenGB)
	tMid := t0.Add(30 * time.Minute)
	p1 := alloc10G.Allocate(t0, tMid)
	p2 := alloc10G.Allocate(tMid, t1)

	if p1 < 0 || p2 < 0 {
		t.Fatalf("Allocated bytes cannot be negative: p1=%d, p2=%d", p1, p2)
	}
	if p1+p2 != tenGB {
		t.Fatalf("10GB split sum mismatch: p1=%d + p2=%d = %d != %d", p1, p2, p1+p2, tenGB)
	}
	halfDiff := math.Abs(float64(p1 - p2))
	if halfDiff > 10 { // 纳秒整除误差应当极小
		t.Fatalf("10GB half split asymmetric: p1=%d, p2=%d", p1, p2)
	}

	// 2. 1 byte 跨两个窗口，两窗口之和必须严格等于 1 byte
	alloc1B := NewIntervalAllocator(t0, t1, 1)
	b1 := alloc1B.Allocate(t0, tMid)
	b2 := alloc1B.Allocate(tMid, t1)
	if b1 < 0 || b2 < 0 || b1+b2 != 1 {
		t.Fatalf("1 byte split mismatch: b1=%d, b2=%d, sum=%d", b1, b2, b1+b2)
	}

	// 3. int64 大值乘纳秒无溢出测试 (例如 8 * 10^18 字节)
	const hugeBytes int64 = 8000000000000000000
	allocHuge := NewIntervalAllocator(t0, t1, hugeBytes)
	h1 := allocHuge.Allocate(t0, tMid)
	h2 := allocHuge.Allocate(tMid, t1)
	if h1 < 0 || h2 < 0 || h1+h2 != hugeBytes {
		t.Fatalf("Huge bytes allocation broken: h1=%d, h2=%d, sum=%d (expected %d)", h1, h2, h1+h2, hugeBytes)
	}

	// 4. 任意 Partition Additive Invariant 测试
	// 随机切成 50 个时间片段，片段之和严格恒等于 F(end) - F(start)
	r := rand.New(rand.NewSource(42))
	var partitionTimes []time.Time
	partitionTimes = append(partitionTimes, t0)
	for i := 0; i < 49; i++ {
		sec := r.Int63n(3599) + 1
		partitionTimes = append(partitionTimes, t0.Add(time.Duration(sec)*time.Second))
	}
	partitionTimes = append(partitionTimes, t1)
	// 排序
	for i := 0; i < len(partitionTimes)-1; i++ {
		for j := i + 1; j < len(partitionTimes); j++ {
			if partitionTimes[j].Before(partitionTimes[i]) {
				partitionTimes[i], partitionTimes[j] = partitionTimes[j], partitionTimes[i]
			}
		}
	}

	var sumAllocated int64
	for i := 0; i < len(partitionTimes)-1; i++ {
		seg := alloc10G.Allocate(partitionTimes[i], partitionTimes[i+1])
		if seg < 0 {
			t.Fatalf("Partition segment %d is negative: %d", i, seg)
		}
		sumAllocated += seg
	}
	if sumAllocated != tenGB {
		t.Fatalf("Additive partition invariant broken! Sum=%d, expected=%d", sumAllocated, tenGB)
	}
}

func TestRelay100BVs1000BSameNodeNotConfirmed(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-relay-small", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	// Frame 1: Bootstrap 初始快照 (Frame 1, Seq 1~2)
	// Candidate: 100B 流量 (缺少 process + rule)
	if err := sink.Emit(&types.CollectorEvent{
		EventID: "c-boot", SessionID: "sess-relay-small", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "cand-small",
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
		Metadata: types.RawMetadata{Process: "", DestinationIP: "1.1.1.1"},
		Chains: []string{"Node-HK"},
	}); err != nil { t.Fatalf("Emit c-boot failed: %v", err) }

	// Logical: 1000B 流量 (同出站节点 Node-HK，但流量差异高达 10 倍)
	if err := sink.Emit(&types.CollectorEvent{
		EventID: "l-boot", SessionID: "sess-relay-small", EpochID: 1, FrameSequence: 1, EventSequence: 2,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "log-large",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "chrome.exe", Host: "google.com"},
		Rule: "MATCH", RulePayload: "MATCH",
		Chains: []string{"Node-HK", "Proxy-Group"},
	}); err != nil { t.Fatalf("Emit l-boot failed: %v", err) }

	// Frame 2: Delta 增量 (Frame 2, Seq 1~2)
	if err := sink.Emit(&types.CollectorEvent{
		EventID: "c-delta", SessionID: "sess-relay-small", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "cand-small",
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
		Chains: []string{"Node-HK"},
		DeltaUpload: 100, DeltaDownload: 100,
		MonitoredCumulativeUpload: 100, MonitoredCumulativeDownload: 100,
	}); err != nil { t.Fatalf("Emit c-delta failed: %v", err) }

	if err := sink.Emit(&types.CollectorEvent{
		EventID: "l-delta", SessionID: "sess-relay-small", EpochID: 1, FrameSequence: 2, EventSequence: 2,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "log-large",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "chrome.exe", Host: "google.com"},
		Rule: "MATCH", RulePayload: "MATCH",
		Chains: []string{"Node-HK", "Proxy-Group"},
		DeltaUpload: 1000, DeltaDownload: 1000,
		MonitoredCumulativeUpload: 1000, MonitoredCumulativeDownload: 1000,
	}); err != nil { t.Fatalf("Emit l-delta failed: %v", err) }
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	run, err := RebuildAccounting(ctx, db, "test small traffic")
	if err != nil { t.Fatalf("RebuildAccounting failed: %v", err) }

	var status string
	_ = db.QueryRowContext(ctx, "SELECT status FROM relay_relations WHERE run_id = ? AND candidate_connection_id = 'cand-small'", run.RunID).Scan(&status)

	// 100B vs 1000B 流量差异过大且低于门槛，绝对不可判定为 confirmed！
	if status == "confirmed" {
		t.Fatalf("100B vs 1000B same-node pair must NOT be confirmed! Got status: %s", status)
	}

	// 验证 accounted PROXY upload 必须是 1100 (不扣减)
	svc := NewAnalyticsService(db)
	summary, err := svc.GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil { t.Fatalf("GetUsageSummary failed: %v", err) }
	if summary.ProxyUpload != 1100 {
		t.Errorf("ProxyUpload must remain 1100 without false deduplication, got %d", summary.ProxyUpload)
	}
}

func TestHistoricalEventMetadataNoFallbackWhenCleared(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-clear-meta", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC)

	// ConnectionNew: 带完整元数据
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "clr-new", SessionID: "sess-clear-meta", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c-clr",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "browser.exe", Host: "example.com"},
		Rule: "RuleA", RulePayload: "PayloadA",
		Chains: []string{"Node1"},
		DeltaUpload: 50, DeltaDownload: 50,
	})

	// ConnectionDelta: Host / Rule / Chains 被清空为 ""
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "clr-delta", SessionID: "sess-clear-meta", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-clr",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "", Host: ""},
		Rule: "", RulePayload: "",
		Chains: nil,
		DeltaUpload: 100, DeltaDownload: 100,
	})
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	run, err := RebuildAccounting(ctx, db, "test clear meta")
	if err != nil { t.Fatalf("RebuildAccounting failed: %v", err) }

	var host1, host2, rule2 string
	_ = db.QueryRowContext(ctx, "SELECT host FROM accounted_traffic WHERE run_id = ? AND source_event_id = 'clr-new'", run.RunID).Scan(&host1)
	_ = db.QueryRowContext(ctx, "SELECT host, rule FROM accounted_traffic WHERE run_id = ? AND source_event_id = 'clr-delta'", run.RunID).Scan(&host2, &rule2)

	if host1 != "example.com" {
		t.Errorf("Expected host1 to be example.com, got '%s'", host1)
	}
	// 验证当前事件清空时，绝不从旧值 fallback！(指令 3)
	if host2 != "" || rule2 != "" {
		t.Errorf("Cleared metadata must remain empty in accounted_traffic! Got host2='%s', rule2='%s'", host2, rule2)
	}
}

func TestAnalyticsControllerGapPhysicalDeltaAllocated(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	t0 := time.Now().UTC().Add(-2 * time.Hour)
	t1 := t0.Add(1 * time.Hour) // 1 小时 Gap，物理流量 1000 字节
	gStartStr := t0.Format(time.RFC3339Nano)
	gEndStr := t1.Format(time.RFC3339Nano)

	_, _ = db.Exec(`
		INSERT INTO collector_sessions (session_id, started_at, status, created_at, updated_at)
		VALUES ('sess-gap-alloc', ?, 'running', ?, ?);
	`, gStartStr, gStartStr, gStartStr)

	_, _ = db.Exec(`
		INSERT INTO monitoring_gaps (
			gap_id, source, session_id, started_at, ended_at, duration_ms, reason,
			global_gap_upload_delta, global_gap_download_delta, precision, created_at
		) VALUES ('gap-1', 'controller_stream', 'sess-gap-alloc', ?, ?, 3600000, 'reconnect', 1000, 2000, 'interval', ?);
	`, gStartStr, gEndStr, gStartStr)

	// 插入一次 accounting run
	_, _ = db.Exec(`
		INSERT INTO accounting_runs (run_id, algorithm_version, started_at, status, source_journal_event_count, source_boundary_json)
		VALUES ('run-gap', 'reconciled-accounting-v1', ?, 'completed', 0, '{}');
	`, gStartStr)

	svc := NewAnalyticsService(db)

	// 查询仅占 Gap 一半时间 (30 分钟) 的窗口
	qStart := t0
	qEnd := t0.Add(30 * time.Minute)

	summary, err := svc.GetUsageSummary(ctx, AnalyticsFilter{StartTime: &qStart, EndTime: &qEnd})
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}

	// 验证 Controller Gap 物理流量使用同一 IntervalAllocator 精确分摊为 500 / 1000，绝不冒充 1000 / 2000！(指令 4)
	if summary.ControllerGapPhysicalUpload != 500 || summary.ControllerGapPhysicalDownload != 1000 {
		t.Errorf("Partial Controller Gap physical traffic mismatch: expected (500, 1000), got (%d, %d)",
			summary.ControllerGapPhysicalUpload, summary.ControllerGapPhysicalDownload)
	}
}

func TestStorageFullMigrationChainV1ToV5(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

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

	evDis := &types.CollectorEvent{
		EventID: "e3", SessionID: "sess-lifecycle", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDisappeared, Timestamp: t0.Add(5 * time.Second), ConnectionID: "conn-disappear",
	}
	if err := sink.Emit(evDis); err != nil { t.Fatalf("Emit disappeared failed: %v", err) }

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

func TestInterruptedSessionLifecycleLastEventPriority(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-interrupted", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	tEvent := time.Date(2026, 8, 25, 8, 15, 0, 0, time.UTC)

	_ = sink.Emit(&types.CollectorEvent{
		EventID: "i-boot", SessionID: "sess-interrupted", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-interrupted",
		Metadata: types.RawMetadata{Process: "task.exe"},
	})
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "i-delta", SessionID: "sess-interrupted", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: tEvent, ConnectionID: "c-interrupted",
		DeltaUpload: 500, DeltaDownload: 500,
	})

	if err := sink.EndSession(ctx, "sess-interrupted", SessionStatusInterrupted); err != nil {
		t.Fatalf("EndSession interrupted failed: %v", err)
	}

	qs := NewQueryService(sink.db)
	cBefore, err := qs.GetConnection(ctx, "sess-interrupted", 1, "c-interrupted")
	if err != nil { t.Fatalf("Get connection failed: %v", err) }

	if cBefore.ObservationEndedAt == nil || !cBefore.ObservationEndedAt.Equal(tEvent) {
		t.Errorf("ObservationEndedAt expected %v, got %v", tEvent, cBefore.ObservationEndedAt)
	}
	if cBefore.ObservationEndReason != "collector_session_interrupted" {
		t.Errorf("ObservationEndReason expected collector_session_interrupted, got %s", cBefore.ObservationEndReason)
	}

	if err := RebuildProjections(ctx, sink.db); err != nil {
		t.Fatalf("RebuildProjections failed: %v", err)
	}
	cAfter, _ := qs.GetConnection(ctx, "sess-interrupted", 1, "c-interrupted")
	if cAfter.ObservationEndedAt == nil || !cAfter.ObservationEndedAt.Equal(tEvent) || cAfter.ObservationEndReason != "collector_session_interrupted" {
		t.Errorf("Rebuild lifecycle mismatch: before=%+v, after=%+v", cBefore, cAfter)
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

	// Frame 1: Bootstrap 初始快照 (Frame 1, Seq 1~6)
	// 1. Logical 连接 (Chrome -> PROXY -> Node-HK -> Google, 有 Process + Rule)
	emitCheck(&types.CollectorEvent{
		EventID: "r-boot-log", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-log-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		ObservedUploadCounter: 100, ObservedDownloadCounter: 200,
		Metadata: types.RawMetadata{Process: "chrome.exe", Host: "google.com", Network: "tcp"},
		Rule: "MATCH", RulePayload: "MATCH",
		Chains: []string{"Node-HK", "Proxy-Group"},
	})
	// 2. Candidate 底层中继连接 (Process 与 Rule 为空, 结构与出站节点一致)
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
		Rule: "DIRECT", RulePayload: "DIRECT",
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
		Metadata: types.RawMetadata{Process: "app1.exe"}, Rule: "MATCH", Chains: []string{"Node-US", "Group-US"},
	})
	// 6. Ambiguous logical 2
	emitCheck(&types.CollectorEvent{
		EventID: "amb-log2-boot", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 1, EventSequence: 6,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-amb-log2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "app2.exe"}, Rule: "MATCH", Chains: []string{"Node-US", "Group-US"},
	})

	// Frame 2: 所有连接的增量 Delta (Frame 2, Seq 1~6, 均 > 500B 满足门槛)
	emitCheck(&types.CollectorEvent{
		EventID: "r-delta-log", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-log-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "chrome.exe", Host: "google.com"}, Rule: "MATCH", Chains: []string{"Node-HK", "Proxy-Group"},
		DeltaUpload: 1000, DeltaDownload: 2000, ObservedUploadCounter: 1100, ObservedDownloadCounter: 2200,
		MonitoredCumulativeUpload: 1000, MonitoredCumulativeDownload: 2000,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "r-delta-cand", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 2,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-cand-1",
		Route: types.RouteProxy, AttributionClass: types.ClassConfirmedRelayDuplicate,
		Chains: []string{"Node-HK"},
		DeltaUpload: 1000, DeltaDownload: 2000, ObservedUploadCounter: 1100, ObservedDownloadCounter: 2200,
		MonitoredCumulativeUpload: 1000, MonitoredCumulativeDownload: 2000,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "d-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 3,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-direct-1",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "curl.exe"}, Rule: "DIRECT", Chains: []string{"DIRECT"},
		DeltaUpload: 500, DeltaDownload: 500, ObservedUploadCounter: 510, ObservedDownloadCounter: 510,
		MonitoredCumulativeUpload: 500, MonitoredCumulativeDownload: 500,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "amb-cand-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 4,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-amb-cand",
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate, Chains: []string{"Node-US"},
		DeltaUpload: 600, DeltaDownload: 700, MonitoredCumulativeUpload: 600, MonitoredCumulativeDownload: 700,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "amb-log1-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 5,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-amb-log1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "app1.exe"}, Rule: "MATCH", Chains: []string{"Node-US", "Group-US"},
		DeltaUpload: 600, DeltaDownload: 700, MonitoredCumulativeUpload: 600, MonitoredCumulativeDownload: 700,
	})
	emitCheck(&types.CollectorEvent{
		EventID: "amb-log2-delta", SessionID: "sess-relay-test", EpochID: 1, FrameSequence: 2, EventSequence: 6,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-amb-log2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "app2.exe"}, Rule: "MATCH", Chains: []string{"Node-US", "Group-US"},
		DeltaUpload: 600, DeltaDownload: 700, MonitoredCumulativeUpload: 600, MonitoredCumulativeDownload: 700,
	})

	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	if _, err := RebuildAccounting(ctx, db, "test relay run"); err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}

	analyticsSvc := NewAnalyticsService(db)
	summary, err := analyticsSvc.GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}

	// 验证：
	// Raw PROXY Up: 3800 (c-log 1000 + c-cand 1000 + amb-cand 600 + amb-log1 600 + amb-log2 600)
	// Accounted PROXY Up: 2800 (c-cand-1 去重为 0，ambiguous 均不扣减)
	// DIRECT Up: 500
	if summary.ProxyUpload != 2800 || summary.ProxyDownload != 4100 {
		t.Errorf("Proxy upload/download mismatch: Up=%d (expected 2800), Down=%d (expected 4100)", summary.ProxyUpload, summary.ProxyDownload)
	}
	if summary.DirectUpload != 500 || summary.DirectDownload != 500 {
		t.Errorf("Direct upload/download mismatch: Up=%d, Down=%d", summary.DirectUpload, summary.DirectDownload)
	}
	if summary.AmbiguousRelayUpload != 600 || summary.AmbiguousRelayDownload != 700 {
		t.Errorf("Ambiguous relay mismatch: Up=%d, Down=%d", summary.AmbiguousRelayUpload, summary.AmbiguousRelayDownload)
	}

	// 验证 10 次确定性 Rebuild
	for i := 0; i < 10; i++ {
		reRun, err := RebuildAccounting(ctx, db, fmt.Sprintf("determinism %d", i))
		if err != nil {
			t.Fatalf("Rebuild iteration %d failed: %v", i, err)
		}
		var accSum int64
		_ = db.QueryRowContext(ctx, "SELECT SUM(accounted_upload + accounted_download) FROM accounted_traffic WHERE run_id = ?", reRun.RunID).Scan(&accSum)
		expectedSum := (2800 + 4100) + (500 + 500)
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
		DeltaUpload: 10000000001, DeltaDownload: 1,
		MonitoredCumulativeUpload: 10000000001, MonitoredCumulativeDownload: 1,
	})
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	run, err := RebuildAccounting(ctx, db, "test alloc")
	if err != nil { t.Fatalf("RebuildAccounting failed: %v", err) }

	var sumUp, sumDown int64
	err = db.QueryRowContext(ctx, `
		SELECT SUM(upload_bytes), SUM(download_bytes)
		FROM usage_hourly_dimensions
		WHERE run_id = ? AND dimension_type = 'total';
	`, run.RunID).Scan(&sumUp, &sumDown)
	if err != nil {
		t.Fatalf("Query hourly sum failed: %v", err)
	}

	if sumUp != 10000000001 || sumDown != 1 {
		t.Fatalf("Byte conservation broken! Expected (10000000001, 1), got (%d, %d)", sumUp, sumDown)
	}
}

func TestMonitoringCoverageIntervalUnionAndTrailingOffline(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	tSessStart := time.Now().UTC().Add(-2 * time.Hour)
	tSessEnd := tSessStart.Add(30 * time.Minute)
	sessStartStr := tSessStart.Format(time.RFC3339Nano)
	sessEndStr := tSessEnd.Format(time.RFC3339Nano)

	_, _ = db.Exec(`
		INSERT INTO collector_sessions (session_id, started_at, ended_at, last_event_at, status, created_at, updated_at)
		VALUES ('sess-cov', ?, ?, ?, 'closed_clean', ?, ?);
	`, sessStartStr, sessEndStr, sessEndStr, sessStartStr, sessStartStr)

	g1Start := tSessStart
	g1End := tSessStart.Add(10 * time.Minute)
	g2Start := tSessStart.Add(5 * time.Minute)
	g2End := tSessStart.Add(15 * time.Minute)

	_, _ = db.Exec(`
		INSERT INTO monitoring_gaps (gap_id, source, session_id, started_at, ended_at, duration_ms, reason, precision, created_at)
		VALUES
			('g1', 'controller_stream', 'sess-cov', ?, ?, 600000, 'reconnect', 'interval', ?),
			('g2', 'collector_session_boundary', 'sess-cov', ?, ?, 600000, 'offline', 'interval', ?);
	`, g1Start.Format(time.RFC3339Nano), g1End.Format(time.RFC3339Nano), sessStartStr,
		g2Start.Format(time.RFC3339Nano), g2End.Format(time.RFC3339Nano), sessStartStr)

	svc := NewAnalyticsService(db)

	qStart := tSessStart
	qEnd := tSessStart.Add(1 * time.Hour)

	cov, err := svc.GetCoverage(ctx, &qStart, &qEnd)
	if err != nil {
		t.Fatalf("GetCoverage failed: %v", err)
	}

	if len(cov.MergedGaps) < 2 {
		t.Errorf("Expected at least 2 merged gaps, got %d: %+v", len(cov.MergedGaps), cov.MergedGaps)
	}
	if len(cov.MergedGaps[0].Sources) < 2 {
		t.Errorf("Expected merged gap 0 to retain multiple sources provenance, got %+v", cov.MergedGaps[0])
	}

	futureStart := time.Now().UTC().Add(1 * time.Hour)
	futureEnd := time.Now().UTC().Add(2 * time.Hour)
	futureCov, err := svc.GetCoverage(ctx, &futureStart, &futureEnd)
	if err != nil {
		t.Fatalf("GetCoverage future failed: %v", err)
	}
	if futureCov.CoverageRatio != nil || futureCov.FutureDurationMs <= 0 {
		t.Errorf("Future coverage ratio must be nil with FutureDurationMs > 0, got ratio=%v, futureMs=%d", futureCov.CoverageRatio, futureCov.FutureDurationMs)
	}
}

func TestAnalyticsServiceTopQueriesAndPartialHour(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-analytics", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC)

	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-boot-1", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "git.exe", Host: "github.com", Network: "tcp"},
		Rule: "MATCH", RulePayload: "MATCH", Chains: []string{"US-Proxy-1", "DefaultGroup"},
	}); err != nil { t.Fatalf("Emit a-boot-1 failed: %v", err) }

	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-boot-2", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 1, EventSequence: 2,
		Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "curl.exe", Host: "api.anthropic.com", Network: "tcp"},
		Rule: "MATCH", RulePayload: "MATCH", Chains: []string{"JP-Proxy-2", "DefaultGroup"},
	}); err != nil { t.Fatalf("Emit a-boot-2 failed: %v", err) }

	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-delta-1", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "git.exe", Host: "github.com", Network: "tcp"},
		Rule: "MATCH", RulePayload: "MATCH", Chains: []string{"US-Proxy-1", "DefaultGroup"},
		DeltaUpload: 2000, DeltaDownload: 8000,
		MonitoredCumulativeUpload: 2000, MonitoredCumulativeDownload: 8000,
	}); err != nil { t.Fatalf("Emit a-delta-1 failed: %v", err) }

	if err := sink.Emit(&types.CollectorEvent{
		EventID: "a-delta-2", SessionID: "sess-analytics", EpochID: 1, FrameSequence: 2, EventSequence: 2,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-2",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "curl.exe", Host: "api.anthropic.com", Network: "tcp"},
		Rule: "MATCH", RulePayload: "MATCH", Chains: []string{"JP-Proxy-2", "DefaultGroup"},
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

	topProcs, err := svc.GetTopProcesses(ctx, AnalyticsFilter{Limit: 10})
	if err != nil || len(topProcs) < 2 {
		t.Fatalf("GetTopProcesses failed: %v, len=%d", err, len(topProcs))
	}
	if topProcs[0].Key != "git.exe" || topProcs[0].TotalBytes != 10000 {
		t.Errorf("Top process #1 mismatch: got %+v", topProcs[0])
	}
	if topProcs[0].ConnectionCount != 1 {
		t.Errorf("Top process distinct connection count mismatch: expected 1, got %d", topProcs[0].ConnectionCount)
	}

	topHosts, err := svc.GetTopHosts(ctx, AnalyticsFilter{Limit: 10})
	if err != nil || len(topHosts) < 2 {
		t.Fatalf("GetTopHosts failed: %v", err)
	}
	if topHosts[0].Key != "github.com" {
		t.Errorf("Top host #1 mismatch: got %+v", topHosts[0])
	}

	topProxies, err := svc.GetTopFinalProxies(ctx, AnalyticsFilter{Limit: 10})
	if err != nil || len(topProxies) < 2 {
		t.Fatalf("GetTopFinalProxies failed: %v", err)
	}
	if topProxies[0].Key != "US-Proxy-1" {
		t.Errorf("Top proxy #1 mismatch: got %+v", topProxies[0])
	}

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

	const count = 5000
	for i := 0; i < count; i++ {
		connID := fmt.Sprintf("c-10k-%d", i)
		_ = sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("b-%d", i), SessionID: "sess-10k", EpochID: 1, FrameSequence: 1, EventSequence: int64(i + 1),
			Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: connID,
			Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: fmt.Sprintf("proc-%d.exe", i%20), Host: fmt.Sprintf("host-%d.com", i%50)},
			Rule: "MATCH", RulePayload: "MATCH",
			Chains: []string{fmt.Sprintf("Node-%d", i%10), "Group-Default"},
		})
	}
	for i := 0; i < count; i++ {
		connID := fmt.Sprintf("c-10k-%d", i)
		_ = sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("d-%d", i), SessionID: "sess-10k", EpochID: 1, FrameSequence: 2, EventSequence: int64(i + 1),
			Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: connID,
			Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: fmt.Sprintf("proc-%d.exe", i%20), Host: fmt.Sprintf("host-%d.com", i%50)},
			Rule: "MATCH", RulePayload: "MATCH",
			Chains: []string{fmt.Sprintf("Node-%d", i%10), "Group-Default"},
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
