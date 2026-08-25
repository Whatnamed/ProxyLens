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

func createRuntimeTestDB(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "proxylens-runtime-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(dir, "runtime-test.db")
	return dbPath, func() {
		_ = os.RemoveAll(dir)
	}
}

// 1. 半开区间 [start, end) 严格边界测试 (F0.3)
func TestAnalyticsHalfOpenWindowBoundary(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-half-open", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t10 := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	t11 := time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC)
	t12 := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	// 事件恰好落在 11:00:00.000
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ev-1100", SessionID: "sess-half-open", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t11, ConnectionID: "c-1100",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "curl.exe", Host: "test.org"},
		Rule: "MATCH", RulePayload: "MATCH",
		DeltaUpload: 100, DeltaDownload: 200,
	})
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	if _, err := RebuildAccounting(ctx, db, "test boundary"); err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}

	svc := NewAnalyticsService(db)

	// 窗口 1: [10:00, 11:00) -> 不应包含 11:00 事件
	sum1, err := svc.GetUsageSummary(ctx, AnalyticsFilter{StartTime: &t10, EndTime: &t11})
	if err != nil { t.Fatalf("GetUsageSummary 1 failed: %v", err) }
	if sum1.UniqueObservedUpload != 0 || sum1.UniqueObservedDownload != 0 {
		t.Errorf("[10:00, 11:00) must NOT include event exactly at 11:00! Got Up=%d, Down=%d", sum1.UniqueObservedUpload, sum1.UniqueObservedDownload)
	}

	// 窗口 2: [11:00, 12:00) -> 必须包含 11:00 事件
	sum2, err := svc.GetUsageSummary(ctx, AnalyticsFilter{StartTime: &t11, EndTime: &t12})
	if err != nil { t.Fatalf("GetUsageSummary 2 failed: %v", err) }
	if sum2.UniqueObservedUpload != 100 || sum2.UniqueObservedDownload != 200 {
		t.Errorf("[11:00, 12:00) MUST include event at 11:00! Got Up=%d, Down=%d", sum2.UniqueObservedUpload, sum2.UniqueObservedDownload)
	}
}

// 2. Migration 006 & 007 Sequence 回填、单调性与心跳测试 (F1 & F4)
func TestStorageMigration006And007BackfillAndMonotonicity(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-seq", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Now().UTC()
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "seq-1", SessionID: "sess-seq", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c1",
		Route: types.RouteProxy, DeltaUpload: 10, DeltaDownload: 20,
	})
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "seq-2", SessionID: "sess-seq", EpochID: 1, FrameSequence: 1, EventSequence: 2,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c2",
		Route: types.RouteProxy, DeltaUpload: 30, DeltaDownload: 40,
	})
	// 发送重复事件 (幂等，不消耗 sequence)
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "seq-2", SessionID: "sess-seq", EpochID: 1, FrameSequence: 1, EventSequence: 2,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c2",
		Route: types.RouteProxy, DeltaUpload: 30, DeltaDownload: 40,
	})
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "seq-3", SessionID: "sess-seq", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c1",
		Route: types.RouteProxy, DeltaUpload: 50, DeltaDownload: 60,
	})
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	// 验证 sequence 分配
	var maxSeq, countSeq int64
	_ = db.QueryRowContext(ctx, "SELECT MAX(journal_sequence), COUNT(journal_sequence) FROM event_journal;").Scan(&maxSeq, &countSeq)
	if maxSeq != 3 || countSeq != 3 {
		t.Errorf("Expected max sequence 3 and count 3, got max=%d, count=%d", maxSeq, countSeq)
	}

	// 验证 session 心跳字段
	var lastHb, hbInt sql.NullString
	_ = db.QueryRowContext(ctx, "SELECT last_heartbeat_at, heartbeat_interval_ms FROM collector_sessions WHERE session_id = 'sess-seq';").Scan(&lastHb, &hbInt)
	if !lastHb.Valid || lastHb.String == "" {
		t.Errorf("Expected valid last_heartbeat_at in session")
	}
}

// 3. 非阻塞 Accounting Rebuild 与并发 Collector 写入及 Freshness 验证 (F2 & F3 & F8)
func TestNonBlockingAccountingRebuildWithConcurrentCollector(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-rebuild-concur", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC)

	// 写入初始 10 个事件 (Boundary = 10)
	for i := 1; i <= 10; i++ {
		_ = sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("ev-init-%d", i), SessionID: "sess-rebuild-concur", EpochID: 1,
			FrameSequence: int64(i), EventSequence: 1,
			Type: types.EventConnectionNew, Timestamp: t0.Add(time.Duration(i) * time.Second),
			ConnectionID: fmt.Sprintf("c-init-%d", i),
			Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: "app.exe", Host: "init.com"},
			Rule: "MATCH", RulePayload: "MATCH",
			DeltaUpload: 100, DeltaDownload: 100,
		})
	}

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	// 执行第一次 Rebuild (此时 Boundary 捕获为 10)
	run1, err := RebuildAccounting(ctx, db, "run 1")
	if err != nil { t.Fatalf("RebuildAccounting 1 failed: %v", err) }

	if *run1.SourceJournalSequenceMax != 10 {
		t.Errorf("Run 1 expected boundary 10, got %d", *run1.SourceJournalSequenceMax)
	}

	// 并发：Collector 继续写入新事件 11~15 (Sequence 11~15)
	for i := 11; i <= 15; i++ {
		_ = sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("ev-later-%d", i), SessionID: "sess-rebuild-concur", EpochID: 1,
			FrameSequence: int64(i), EventSequence: 1,
			Type: types.EventConnectionNew, Timestamp: t0.Add(time.Duration(i) * time.Second),
			ConnectionID: fmt.Sprintf("c-later-%d", i),
			Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: "app.exe", Host: "later.com"},
			Rule: "MATCH", RulePayload: "MATCH",
			DeltaUpload: 200, DeltaDownload: 200,
		})
	}
	_ = sink.Close()

	svc := NewAnalyticsService(db)

	// 验证 Run 1 的用量只有前 10 个事件 (10 * 100 = 1000B)
	sum1, err := svc.GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil { t.Fatalf("GetUsageSummary failed: %v", err) }
	if sum1.UniqueObservedUpload != 1000 {
		t.Errorf("Run 1 should only contain 1000B, got %d", sum1.UniqueObservedUpload)
	}

	// 验证 Freshness 明确显示过时 (Stale, Lag = 5)
	freshness1, err := svc.GetAccountingFreshness(ctx)
	if err != nil { t.Fatalf("GetAccountingFreshness failed: %v", err) }
	if freshness1.IsFresh {
		t.Errorf("Expected freshness to be STALE, got fresh")
	}
	if freshness1.LagEvents != 5 {
		t.Errorf("Expected lag 5 events, got %d", freshness1.LagEvents)
	}

	// 执行第二次 Rebuild (追平到 Sequence 15)
	run2, err := RebuildAccounting(ctx, db, "run 2")
	if err != nil { t.Fatalf("RebuildAccounting 2 failed: %v", err) }

	if *run2.SourceJournalSequenceMax != 15 {
		t.Errorf("Run 2 expected boundary 15, got %d", *run2.SourceJournalSequenceMax)
	}

	freshness2, err := svc.GetAccountingFreshness(ctx)
	if err != nil { t.Fatalf("GetAccountingFreshness 2 failed: %v", err) }
	if !freshness2.IsFresh || freshness2.LagEvents != 0 {
		t.Errorf("Run 2 should be fresh with lag 0, got fresh=%v, lag=%d", freshness2.IsFresh, freshness2.LagEvents)
	}

	// 验证总流量追平为 10*100 + 5*200 = 2000B
	sum2, err := svc.GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil { t.Fatalf("GetUsageSummary 2 failed: %v", err) }
	if sum2.UniqueObservedUpload != 2000 {
		t.Errorf("Run 2 total upload should be 2000, got %d", sum2.UniqueObservedUpload)
	}
}

// 4. Collector 心跳超时与 Hard-Kill 后的 Coverage Liveness 动态推导 (F4)
func TestCollectorHeartbeatAndRuntimeLivenessCoverage(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	// 模拟一个处于 running 状态但心跳在 1 小时前停止的 Session (模拟 hard kill)
	t0 := time.Now().UTC().Add(-2 * time.Hour)
	tLastHb := time.Now().UTC().Add(-1 * time.Hour)

	_, _ = db.Exec(`
		INSERT INTO collector_sessions (
			session_id, started_at, status, last_heartbeat_at, heartbeat_interval_ms, created_at, updated_at
		) VALUES (
			'sess-crashed', ?, 'running', ?, 5000, ?, ?
		);
	`, t0.Format(time.RFC3339Nano), tLastHb.Format(time.RFC3339Nano), t0.Format(time.RFC3339Nano), t0.Format(time.RFC3339Nano))

	svc := NewAnalyticsService(db)
	cov, err := svc.GetCoverage(ctx, &t0, nil)
	if err != nil {
		t.Fatalf("GetCoverage failed: %v", err)
	}

	// 验证动态产生 collector_runtime_liveness / collector_heartbeat_stale 缺口，绝不假装在线！
	var foundLivenessGap bool
	for _, g := range cov.MergedGaps {
		for _, r := range g.Reasons {
			if r == "collector_heartbeat_stale" {
				foundLivenessGap = true
			}
		}
	}

	if !foundLivenessGap {
		t.Fatalf("Coverage must dynamically flag collector_heartbeat_stale gap for crashed running session!")
	}
}

// 5. Safe Derived Retention 绝不删除原始权威数据测试 (F5)
func TestSafeDerivedRetentionNeverDeletesRawAuthority(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-retention", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		_ = sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("ev-raw-%d", i), SessionID: "sess-retention", EpochID: 1,
			FrameSequence: int64(i), EventSequence: 1,
			Type: types.EventConnectionNew, Timestamp: t0.Add(time.Duration(i) * time.Minute),
			ConnectionID: fmt.Sprintf("c-%d", i),
			Route: types.RouteProxy, DeltaUpload: 100, DeltaDownload: 100,
		})
	}
	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	// 生成 5 次 accounting runs
	for i := 1; i <= 5; i++ {
		time.Sleep(10 * time.Millisecond)
		_, err := RebuildAccounting(ctx, db, fmt.Sprintf("run %d", i))
		if err != nil { t.Fatalf("Rebuild %d failed: %v", i, err) }
	}

	// 计划保留最新的 3 个 completed runs
	plan, err := PlanDerivedRetention(ctx, db, 3, 7*24*time.Hour, dbPath)
	if err != nil { t.Fatalf("PlanDerivedRetention failed: %v", err) }

	if len(plan.RunsToDelete) != 2 {
		t.Fatalf("Expected 2 runs marked for deletion, got %d", len(plan.RunsToDelete))
	}

	// 执行清理
	res, err := ApplyDerivedRetention(ctx, db, plan)
	if err != nil { t.Fatalf("ApplyDerivedRetention failed: %v", err) }
	if res.DeletedRuns != 2 {
		t.Fatalf("Expected 2 runs deleted, got %d", res.DeletedRuns)
	}

	// 严格核查：原始权威数据 100% 完整保留！
	var rawJournalCount, rawTrafficCount int64
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM event_journal;").Scan(&rawJournalCount)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM connection_traffic;").Scan(&rawTrafficCount)

	if rawJournalCount != 5 || rawTrafficCount != 5 {
		t.Fatalf("Raw authority MUST NEVER be deleted! Expected 5 raw rows, got journal=%d, traffic=%d", rawJournalCount, rawTrafficCount)
	}
}

// 6. PRODUCT A–E 确定性合约验收场景测试套件 (F10)
func TestProductAcceptanceDeterministic(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-acceptance", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)

	// [PRODUCT A] NTP Background Traffic (UDP dst port 123)
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ntp-1", SessionID: "sess-acceptance", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c-ntp",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "w32time.dll", Host: "time.windows.com", DestinationPort: "123", Network: "udp"},
		Rule: "NTP-Rule", RulePayload: "NTP-Payload",
		DeltaUpload: 48, DeltaDownload: 48,
	})

	// [PRODUCT B] Large PROXY (1 GiB = 1,073,741,824 B)
	const oneGiB int64 = 1073741824
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "large-proxy-1", SessionID: "sess-acceptance", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(time.Hour), ConnectionID: "c-proxy-large",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "downloader.exe", Host: "cdn.speedtest.net"},
		Rule: "Speedtest", RulePayload: "Speedtest",
		Chains: []string{"Node-HK-01", "Proxy-Auto"},
		DeltaUpload: 10 * 1024 * 1024, DeltaDownload: oneGiB,
	})

	// [PRODUCT C] Large DIRECT (500 MiB)
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "large-direct-1", SessionID: "sess-acceptance", EpochID: 1, FrameSequence: 3, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(2 * time.Hour), ConnectionID: "c-direct-large",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "steam.exe", Host: "steamcontent.com"},
		Rule: "SteamDirect", RulePayload: "SteamDirect",
		DeltaUpload: 1 * 1024 * 1024, DeltaDownload: 500 * 1024 * 1024,
	})

	// [PRODUCT E] Node Switch History (早晨 HK 节点，下午 JP 节点)
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "sw-hk", SessionID: "sess-acceptance", EpochID: 1, FrameSequence: 4, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(3 * time.Hour), ConnectionID: "c-sw-morning",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "browser.exe", Host: "news.com"},
		Rule: "ProxyRule", RulePayload: "ProxyRule",
		Chains: []string{"Node-HK-01", "Proxy-Group"},
		DeltaUpload: 1000, DeltaDownload: 5000,
	})
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "sw-jp", SessionID: "sess-acceptance", EpochID: 1, FrameSequence: 5, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(7 * time.Hour), ConnectionID: "c-sw-afternoon",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "browser.exe", Host: "news.com"},
		Rule: "ProxyRule", RulePayload: "ProxyRule",
		Chains: []string{"Node-JP-02", "Proxy-Group"},
		DeltaUpload: 2000, DeltaDownload: 8000,
	})

	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	if _, err := RebuildAccounting(ctx, db, "acceptance run"); err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}

	svc := NewAnalyticsService(db)

	// 验收 A: NTP
	topProc, _ := svc.GetTopProcesses(ctx, AnalyticsFilter{})
	var foundNTP bool
	for _, p := range topProc {
		if p.Key == "w32time.dll" {
			foundNTP = true
			if p.Route != types.RouteDirect {
				t.Errorf("NTP route expected DIRECT, got %s", p.Route)
			}
		}
	}
	if !foundNTP {
		t.Errorf("PRODUCT A: NTP process w32time.dll not found in top processes")
	}

	// 验收 B: Large PROXY 1GiB 无溢出与守恒
	summary, _ := svc.GetUsageSummary(ctx, AnalyticsFilter{})
	if summary.ProxyDownload < oneGiB {
		t.Errorf("PRODUCT B: 1GiB Proxy download mismatch: got %d, expected >= %d", summary.ProxyDownload, oneGiB)
	}

	// 验收 C: Large DIRECT 独立
	if summary.DirectDownload < 500*1024*1024 {
		t.Errorf("PRODUCT C: 500MiB Direct download mismatch: got %d", summary.DirectDownload)
	}

	// 验收 E: Node Switch History 早晨 HK，下午 JP 独立归因
	tMorningStart := t0.Add(2 * time.Hour)
	tMorningEnd := t0.Add(4 * time.Hour)
	tAfternoonStart := t0.Add(6 * time.Hour)
	tAfternoonEnd := t0.Add(8 * time.Hour)

	topProxiesMorning, errM := svc.GetTopFinalProxies(ctx, AnalyticsFilter{StartTime: &tMorningStart, EndTime: &tMorningEnd, Route: types.RouteProxy})
	if errM != nil {
		t.Fatalf("GetTopFinalProxies morning failed: %v", errM)
	}
	topProxiesAfternoon, errA := svc.GetTopFinalProxies(ctx, AnalyticsFilter{StartTime: &tAfternoonStart, EndTime: &tAfternoonEnd, Route: types.RouteProxy})
	if errA != nil {
		t.Fatalf("GetTopFinalProxies afternoon failed: %v", errA)
	}

	if len(topProxiesMorning) == 0 || topProxiesMorning[0].Key != "Node-HK-01" {
		t.Errorf("PRODUCT E: Morning proxy should be Node-HK-01, got count=%d, items=%+v", len(topProxiesMorning), topProxiesMorning)
	}
	if len(topProxiesAfternoon) == 0 || topProxiesAfternoon[0].Key != "Node-JP-02" {
		t.Errorf("PRODUCT E: Afternoon proxy should be Node-JP-02, got count=%d, items=%+v", len(topProxiesAfternoon), topProxiesAfternoon)
	}
}
