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

// 5.2 Stale RelayCandidate 回归测试: 当 metadata 后续补齐 process+rule 时，严禁仅凭旧 ClassRelayCandidate 误扣流量
func TestStaleRelayCandidateUpgradedToLogicalWhenMetadataEnriched(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-stale-cand", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)

	// Frame 1: 早期首帧，缺少 process 与 rule，事件被标记为 ClassRelayCandidate
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ev-cand-f1", SessionID: "sess-stale-cand", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c-enriched-1",
		Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
		Metadata: types.RawMetadata{Process: "", Host: "", DestinationIP: "1.2.3.4"},
		Chains: []string{"Node-Alpha"},
		DeltaUpload: 100, DeltaDownload: 500,
	})

	// Frame 2: 后续帧，Mihomo 异步补齐了 Process 与 Rule，标记为 ClassKnownApplication
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ev-cand-f2", SessionID: "sess-stale-cand", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionDelta, Timestamp: t0.Add(time.Second), ConnectionID: "c-enriched-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "curl.exe", Host: "target.org", DestinationIP: "1.2.3.4"},
		Rule: "ProxyRule", RulePayload: "ProxyRule",
		Chains: []string{"Node-Alpha", "ProxyGroup"},
		DeltaUpload: 200, DeltaDownload: 1000,
	})

	_ = sink.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	run, err := RebuildAccounting(ctx, db, "test stale candidate upgrade")
	if err != nil { t.Fatalf("RebuildAccounting failed: %v", err) }

	// 验证：该连接在核算事实中必须是 Unique (不扣减)，不能成为 Candidate 被作为 relay duplicate 扣除！
	var accClass string
	var accountedUp, accountedDown int64
	err = db.QueryRowContext(ctx, `
		SELECT accounting_class, accounted_upload, accounted_download
		FROM accounted_traffic
		WHERE run_id = ? AND connection_id = 'c-enriched-1'
		ORDER BY source_event_id DESC LIMIT 1;
	`, run.RunID).Scan(&accClass, &accountedUp, &accountedDown)
	if err != nil { t.Fatalf("Query accounted_traffic failed: %v", err) }

	if accClass != string(ClassUnique) {
		t.Errorf("Expected accounting_class to be %s, got %s", ClassUnique, accClass)
	}
	if accountedUp != 200 || accountedDown != 1000 {
		t.Errorf("Accounted bytes must match delta (200/1000), got Up=%d, Down=%d", accountedUp, accountedDown)
	}

	// 验证 relay_relations 中不能有 confirmed candidate
	var relCount int64
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM relay_relations WHERE run_id = ? AND status = 'confirmed';", run.RunID).Scan(&relCount)
	if relCount != 0 {
		t.Errorf("Expected 0 confirmed relay relations, got %d", relCount)
	}
}

// 6. PRODUCT A–E 按 PRODUCT.md 原始定义逐项机械断言测试套件 (F10)
func TestProductAcceptanceDeterministic(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createRuntimeTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-acceptance-1", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink failed: %v", err) }

	// 基准时间设置为 24 小时前，确保测试中的所有时间点均属于已发生历史
	t0 := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Hour)

	// -------------------------------------------------------------
	// 场景 A: 后台 NTP 审计 (PRODUCT.md 5.1)
	// 操作: Windows 后台/杀毒软件向 us.pool.ntp.org:123 发起 UDP 同步
	// 验收: 记录完整连接 (进程名 HipsDaemon.exe, 目标 us.pool.ntp.org, 端口 123, UDP, 规则 NETWORK,udp, 策略链路, 节点, 流量)
	// -------------------------------------------------------------
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ntp-1", SessionID: "sess-acceptance-1", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c-ntp-audit",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "HipsDaemon.exe", ProcessPath: "C:\\Program Files\\Security\\HipsDaemon.exe",
			Host: "us.pool.ntp.org", DestinationIP: "198.51.100.1", DestinationPort: "123", Network: "udp",
		},
		Rule: "NETWORK,udp", RulePayload: "udp",
		Chains: []string{"Node-US-NTP", "ProxyGroup"},
		DeltaUpload: 48, DeltaDownload: 48,
	})

	// -------------------------------------------------------------
	// 场景 B: 代理下载大文件追溯 (PRODUCT.md 5.2)
	// 操作: 通过代理节点下载约 1GB 文件 (合成 1,073,741,824 B)
	// 验收: 清晰说明 1GB 流量归属 (进程 downloader.exe, 域名 cdn.speedtest.net, 规则 Speedtest, 策略组 Proxy-Auto, 实际节点 Node-HK-01), 严禁出现大块 Unknown
	// -------------------------------------------------------------
	const oneGiB int64 = 1073741824
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "large-proxy-1", SessionID: "sess-acceptance-1", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(time.Hour), ConnectionID: "c-proxy-large",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "downloader.exe", ProcessPath: "C:\\Tools\\downloader.exe",
			Host: "cdn.speedtest.net", DestinationIP: "151.101.1.1", DestinationPort: "443", Network: "tcp",
		},
		Rule: "Speedtest", RulePayload: "cdn.speedtest.net",
		Chains: []string{"Node-HK-01", "Proxy-Auto"},
		DeltaUpload: 10 * 1024 * 1024, DeltaDownload: oneGiB,
	})

	// -------------------------------------------------------------
	// 场景 C: DIRECT 大流量防误计 (PRODUCT.md 5.3)
	// 操作: 通过国内直连网络下载数 GB 内容 (Steam 游戏下载 2 GiB = 2,147,483,648 B)
	// 验收: 正确分类为 DIRECT 连接，不计入代理节点总消耗 (Route=DIRECT, final_proxy='DIRECT')
	// -------------------------------------------------------------
	const twoGiB int64 = 2147483648
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "large-direct-1", SessionID: "sess-acceptance-1", EpochID: 1, FrameSequence: 3, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(2 * time.Hour), ConnectionID: "c-direct-steam",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "steam.exe", ProcessPath: "C:\\Steam\\steam.exe",
			Host: "steamcontent.com", DestinationIP: "119.29.29.29", DestinationPort: "443", Network: "tcp",
		},
		Rule: "SteamDirect", RulePayload: "steamcontent.com",
		DeltaUpload: 2 * 1024 * 1024, DeltaDownload: twoGiB,
	})

	// -------------------------------------------------------------
	// 场景 E: 节点切换历史追溯 (PRODUCT.md 5.5)
	// 操作: 上午使用节点 A (Node-HK-01) 产生流量，下午切换至节点 B (Node-JP-02) 产生流量
	// 验收: 历史连接必须永久锁定当时发生时所走的真实节点 A 与节点 B，严禁因最新节点为 B 篡改上午记录
	// -------------------------------------------------------------
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "sw-hk", SessionID: "sess-acceptance-1", EpochID: 1, FrameSequence: 4, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(3 * time.Hour), ConnectionID: "c-sw-morning",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "browser.exe", Host: "news.com"},
		Rule: "ProxyRule", RulePayload: "ProxyRule",
		Chains: []string{"Node-HK-01", "Proxy-Group"},
		DeltaUpload: 1000, DeltaDownload: 5000,
	})
	_ = sink.Emit(&types.CollectorEvent{
		EventID: "sw-jp", SessionID: "sess-acceptance-1", EpochID: 1, FrameSequence: 5, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(7 * time.Hour), ConnectionID: "c-sw-afternoon",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "browser.exe", Host: "news.com"},
		Rule: "ProxyRule", RulePayload: "ProxyRule",
		Chains: []string{"Node-JP-02", "Proxy-Group"},
		DeltaUpload: 2000, DeltaDownload: 8000,
	})

	// -------------------------------------------------------------
	// 场景 D: Collector 中断与缺口表达 (PRODUCT.md 5.4)
	// 操作: 停止后台 Collector 进程 1 小时后重新启动 (t0+7h -> t0+8h 离线)
	// 验收: 明确标记这 1 小时为 Monitoring Gap，提示覆盖率下降，严禁推卸或伪装为未知流量
	// -------------------------------------------------------------
	_ = sink.EndSession(ctx, "sess-acceptance-1", SessionStatusClosedClean)
	_ = sink.Close()

	// 1 小时后启动 Session 2 (t0+8h -> t0+9h)
	sink2, err := OpenSQLiteSink(ctx, dbPath, "sess-acceptance-2", "v1.0.0-test")
	if err != nil { t.Fatalf("Open sink 2 failed: %v", err) }
	_ = sink2.Emit(&types.CollectorEvent{
		EventID: "ev-sess2-1", SessionID: "sess-acceptance-2", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(8 * time.Hour), ConnectionID: "c-after-gap",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{Process: "browser.exe", Host: "github.com"},
		Rule: "MATCH", RulePayload: "MATCH",
		Chains: []string{"Node-HK-01", "Proxy-Auto"},
		DeltaUpload: 100, DeltaDownload: 500,
	})
	_ = sink2.EndSession(ctx, "sess-acceptance-2", SessionStatusClosedClean)
	_ = sink2.Close()

	db, err := OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	// 插入确定的 1 小时离线 Gap
	gapStart := t0.Add(7 * time.Hour).Format(time.RFC3339Nano)
	gapEnd := t0.Add(8 * time.Hour).Format(time.RFC3339Nano)
	_, _ = db.ExecContext(ctx, `
		INSERT INTO monitoring_gaps (gap_id, source, session_id, started_at, ended_at, reason, precision, created_at)
		VALUES ('gap-sess1-sess2', 'collector_session_boundary', 'sess-acceptance-2', ?, ?, 'collector_not_running', 'interval', ?);
	`, gapStart, gapEnd, gapEnd)

	// 统一会话时间戳与测试数据一致
	_, _ = db.ExecContext(ctx, "UPDATE collector_sessions SET started_at = ?, ended_at = ? WHERE session_id = 'sess-acceptance-1';",
		t0.Format(time.RFC3339Nano), t0.Add(7*time.Hour).Format(time.RFC3339Nano))
	_, _ = db.ExecContext(ctx, "UPDATE collector_sessions SET started_at = ?, ended_at = ? WHERE session_id = 'sess-acceptance-2';",
		t0.Add(8*time.Hour).Format(time.RFC3339Nano), t0.Add(9*time.Hour).Format(time.RFC3339Nano))

	if _, err := RebuildAccounting(ctx, db, "acceptance verification run"); err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}

	svc := NewAnalyticsService(db)

	// [机械断言 A: NTP 逐字段严格机械断言]
	var ntpDestPort string
	err = db.QueryRowContext(ctx, "SELECT destination_port FROM connections WHERE connection_id = 'c-ntp-audit';").Scan(&ntpDestPort)
	if err != nil {
		t.Fatalf("Scene A assertion failed: destination_port not found in connections: %v", err)
	}

	var ntpRecord AccountedTrafficRecord
	err = db.QueryRowContext(ctx, `
		SELECT connection_id, process, process_path, host, destination_ip, network, rule, rule_payload, final_proxy, raw_upload, raw_download
		FROM accounted_traffic
		WHERE connection_id = 'c-ntp-audit' LIMIT 1;
	`).Scan(&ntpRecord.ConnectionID, &ntpRecord.Process, &ntpRecord.ProcessPath, &ntpRecord.Host,
		&ntpRecord.DestinationIP, &ntpRecord.Network, &ntpRecord.Rule, &ntpRecord.RulePayload,
		&ntpRecord.FinalProxy, &ntpRecord.RawUpload, &ntpRecord.RawDownload)
	if err != nil {
		t.Fatalf("Scene A assertion failed: NTP connection not found in accounted_traffic: %v", err)
	}
	if ntpRecord.Process != "HipsDaemon.exe" || ntpRecord.Host != "us.pool.ntp.org" || ntpDestPort != "123" || ntpRecord.Network != "udp" || ntpRecord.Rule != "NETWORK,udp" || ntpRecord.FinalProxy != "Node-US-NTP" || ntpRecord.RawUpload != 48 || ntpRecord.RawDownload != 48 {
		t.Errorf("Scene A NTP fields mismatch: %+v (port=%s)", ntpRecord, ntpDestPort)
	}

	// [机械断言 B: 大文件代理下载 1GiB 逐字段严格机械断言]
	var proxyLargeDestPort string
	err = db.QueryRowContext(ctx, "SELECT destination_port FROM connections WHERE connection_id = 'c-proxy-large';").Scan(&proxyLargeDestPort)
	if err != nil {
		t.Fatalf("Scene B assertion failed: destination_port not found in connections: %v", err)
	}

	var proxyLargeRecord AccountedTrafficRecord
	err = db.QueryRowContext(ctx, `
		SELECT connection_id, process, process_path, host, destination_ip, network, rule, rule_payload, final_proxy, top_policy_group, raw_download, accounted_download
		FROM accounted_traffic
		WHERE connection_id = 'c-proxy-large' LIMIT 1;
	`).Scan(&proxyLargeRecord.ConnectionID, &proxyLargeRecord.Process, &proxyLargeRecord.ProcessPath,
		&proxyLargeRecord.Host, &proxyLargeRecord.DestinationIP,
		&proxyLargeRecord.Network, &proxyLargeRecord.Rule, &proxyLargeRecord.RulePayload,
		&proxyLargeRecord.FinalProxy, &proxyLargeRecord.TopPolicyGroup,
		&proxyLargeRecord.RawDownload, &proxyLargeRecord.AccountedDownload)
	if err != nil {
		t.Fatalf("Scene B assertion failed: large proxy record not found: %v", err)
	}
	if proxyLargeRecord.Process != "downloader.exe" || proxyLargeRecord.ProcessPath != "C:\\Tools\\downloader.exe" ||
		proxyLargeRecord.Host != "cdn.speedtest.net" || proxyLargeDestPort != "443" ||
		proxyLargeRecord.Rule != "Speedtest" || proxyLargeRecord.RulePayload != "cdn.speedtest.net" ||
		proxyLargeRecord.FinalProxy != "Node-HK-01" || proxyLargeRecord.TopPolicyGroup != "Proxy-Auto" ||
		proxyLargeRecord.RawDownload != oneGiB || proxyLargeRecord.AccountedDownload != oneGiB {
		t.Errorf("Scene B large proxy fields mismatch: %+v (port=%s)", proxyLargeRecord, proxyLargeDestPort)
	}

	summary, err := svc.GetUsageSummary(ctx, AnalyticsFilter{})
	if err != nil { t.Fatalf("GetUsageSummary failed: %v", err) }
	if summary.ProxyDownload < oneGiB {
		t.Errorf("Scene B summary assertion failed: expected ProxyDownload >= 1GiB, got %d", summary.ProxyDownload)
	}
	var unknownCount int64
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounted_traffic WHERE run_id = (SELECT run_id FROM accounting_runs WHERE status='completed' LIMIT 1) AND accounting_class = 'missing_attribution';").Scan(&unknownCount)
	if unknownCount > 0 {
		t.Errorf("Scene B assertion failed: unexpected missing attribution count: %d", unknownCount)
	}

	// [机械断言 C: DIRECT 大流量防误计 (2GiB 必须为 DIRECT，严禁计入 ProxyNode)]
	if summary.DirectDownload < twoGiB {
		t.Errorf("Scene C assertion failed: expected DirectDownload >= 2GiB, got %d", summary.DirectDownload)
	}
	topProxiesAll, _ := svc.GetTopFinalProxies(ctx, AnalyticsFilter{Route: types.RouteProxy})
	for _, p := range topProxiesAll {
		if p.Key == "DIRECT" {
			t.Errorf("Scene C assertion failed: DIRECT leaked into top proxy list: %+v", p)
		}
	}

	// [机械断言 D: Collector 中断显式 Gap 记录与覆盖率下降]
	qStart := t0
	qEnd := t0.Add(9 * time.Hour)
	coverage, err := svc.GetCoverage(ctx, &qStart, &qEnd)
	if err != nil { t.Fatalf("GetCoverage failed: %v", err) }
	if len(coverage.MergedGaps) == 0 {
		t.Errorf("Scene D assertion failed: expected monitoring gap between session 1 and 2, got 0 gaps")
	}
	if coverage.CoverageRatio == nil || *coverage.CoverageRatio >= 1.0 {
		t.Errorf("Scene D assertion failed: expected CoverageRatio < 1.0 due to gap, got %v", coverage.CoverageRatio)
	}

	// [机械断言 E: 节点切换历史锁定，上午 HK，下午 JP]
	tMorningStart := t0.Add(2 * time.Hour)
	tMorningEnd := t0.Add(4 * time.Hour)
	tAfternoonStart := t0.Add(6 * time.Hour)
	tAfternoonEnd := t0.Add(8 * time.Hour)

	topProxiesMorning, errM := svc.GetTopFinalProxies(ctx, AnalyticsFilter{StartTime: &tMorningStart, EndTime: &tMorningEnd, Route: types.RouteProxy})
	if errM != nil { t.Fatalf("GetTopFinalProxies morning failed: %v", errM) }
	topProxiesAfternoon, errA := svc.GetTopFinalProxies(ctx, AnalyticsFilter{StartTime: &tAfternoonStart, EndTime: &tAfternoonEnd, Route: types.RouteProxy})
	if errA != nil { t.Fatalf("GetTopFinalProxies afternoon failed: %v", errA) }

	if len(topProxiesMorning) == 0 || topProxiesMorning[0].Key != "Node-HK-01" {
		t.Errorf("Scene E assertion failed: Morning proxy should be Node-HK-01, got %+v", topProxiesMorning)
	}
	if len(topProxiesAfternoon) == 0 || topProxiesAfternoon[0].Key != "Node-JP-02" {
		t.Errorf("Scene E assertion failed: Afternoon proxy should be Node-JP-02, got %+v", topProxiesAfternoon)
	}
}
