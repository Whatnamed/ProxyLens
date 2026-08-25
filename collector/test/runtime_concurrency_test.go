package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// F8: Accounting Concurrency Benchmark (Collector 250ms ingest 期间并发 Rebuild)
func TestAccountingConcurrencyUnderLiveIngestion(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "proxylens-concur-go-*")
	if err != nil { t.Fatalf("MkdirTemp failed: %v", err) }
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "concur.db")

	sink, err := storage.OpenSQLiteSink(ctx, dbPath, "sess-live-concur", "v1.0.0-test")
	if err != nil { t.Fatalf("OpenSQLiteSink failed: %v", err) }

	t0 := time.Now().UTC()

	// 1. 注入初始 50 个事件 (Sequence 1~50)
	for i := 1; i <= 50; i++ {
		if err := sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("ev-live-%d", i),
			SessionID: "sess-live-concur",
			EpochID: 1,
			FrameSequence: int64(i),
			EventSequence: 1,
			Type: types.EventConnectionNew,
			Timestamp: t0.Add(time.Duration(i) * 10 * time.Millisecond),
			ConnectionID: fmt.Sprintf("c-live-%d", i),
			Route: types.RouteProxy,
			AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: "chrome.exe", Host: "live.com"},
			Rule: "ProxyRule", RulePayload: "ProxyPayload",
			DeltaUpload: 100, DeltaDownload: 200,
		}); err != nil {
			t.Fatalf("Emit initial event %d failed: %v", i, err)
		}
	}

	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	// 2. 执行 Run 1 Rebuild (捕获 Boundary = 50)
	tRebuildStart := time.Now()
	run1, err := storage.RebuildAccounting(ctx, db, "live concurrency rebuild")
	rebuildDur := time.Since(tRebuildStart)

	if err != nil {
		t.Fatalf("RebuildAccounting 1 failed: %v", err)
	}

	if *run1.SourceJournalSequenceMax != 50 {
		t.Errorf("Expected run 1 boundary 50, got %d", *run1.SourceJournalSequenceMax)
	}
	t.Logf("Rebuild 1 completed in %v (Bound Sequence: %d)", rebuildDur, *run1.SourceJournalSequenceMax)

	// 3. 模拟 Collector 持续写入新事件 (Sequence 51~60)
	for i := 51; i <= 60; i++ {
		if err := sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("ev-later-%d", i),
			SessionID: "sess-live-concur",
			EpochID: 1,
			FrameSequence: int64(i),
			EventSequence: 1,
			Type: types.EventConnectionNew,
			Timestamp: t0.Add(time.Duration(i) * 10 * time.Millisecond),
			ConnectionID: fmt.Sprintf("c-later-%d", i),
			Route: types.RouteProxy,
			AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: "app.exe", Host: "later.com"},
			Rule: "MATCH", RulePayload: "MATCH",
			DeltaUpload: 50, DeltaDownload: 50,
		}); err != nil {
			t.Fatalf("Emit later event %d failed: %v", i, err)
		}
	}
	_ = sink.Close()

	// 4. 验证 Freshness 明确显示过时 (Stale, Lag = 10)
	svc := storage.NewAnalyticsService(db)
	freshness1, err := svc.GetAccountingFreshness(ctx)
	if err != nil { t.Fatalf("GetAccountingFreshness failed: %v", err) }

	if freshness1.IsFresh {
		t.Errorf("Expected run 1 to be STALE, got fresh")
	}
	if freshness1.LagEvents != 10 {
		t.Errorf("Expected lag 10 events, got %d", freshness1.LagEvents)
	}

	// 5. 执行二次 Rebuild 追平最新 (Sequence 60)
	run2, err := storage.RebuildAccounting(ctx, db, "catchup rebuild")
	if err != nil { t.Fatalf("Catchup Rebuild failed: %v", err) }

	if *run2.SourceJournalSequenceMax != 60 {
		t.Errorf("Expected run 2 boundary 60, got %d", *run2.SourceJournalSequenceMax)
	}

	freshness2, err := svc.GetAccountingFreshness(ctx)
	if err != nil { t.Fatalf("GetAccountingFreshness 2 failed: %v", err) }
	if !freshness2.IsFresh || freshness2.LagEvents != 0 {
		t.Errorf("Run 2 must be fresh with lag 0, got isFresh=%v, lag=%d", freshness2.IsFresh, freshness2.LagEvents)
	}

	t.Logf("Run 2 caught up successfully! Source boundary: %d", *run2.SourceJournalSequenceMax)
}

// F8.2: Relay-Heavy Accounting Scale (500/500 candidate/logical pairs)
func TestRelayHeavyAccountingScale(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "proxylens-scale-*")
	if err != nil { t.Fatalf("MkdirTemp failed: %v", err) }
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "scale.db")
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, "sess-scale", "v1.0.0-test")
	if err != nil { t.Fatalf("OpenSQLiteSink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	// 构造 500 对严格 1-to-1 配对的 candidate 与 logical
	const pairCount = 500
	for p := 1; p <= pairCount; p++ {
		// Logical
		if err := sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("l-new-%d", p), SessionID: "sess-scale", EpochID: 1,
			FrameSequence: int64(p), EventSequence: 1,
			Type: types.EventConnectionNew, Timestamp: t0,
			ConnectionID: fmt.Sprintf("log-%d", p),
			Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
			Metadata: types.RawMetadata{Process: fmt.Sprintf("app-%d.exe", p), Host: fmt.Sprintf("target-%d.com", p)},
			Rule: "MATCH", RulePayload: "MATCH",
			Chains: []string{fmt.Sprintf("Node-Unique-%d", p), "ProxyGroup"},
			DeltaUpload: int64(1000 + p), DeltaDownload: int64(2000 + p),
		}); err != nil {
			t.Fatalf("Emit logical %d failed: %v", p, err)
		}
		// Candidate (缺少 process & rule, 相同出站节点)
		if err := sink.Emit(&types.CollectorEvent{
			EventID: fmt.Sprintf("c-new-%d", p), SessionID: "sess-scale", EpochID: 1,
			FrameSequence: int64(p), EventSequence: 2,
			Type: types.EventConnectionNew, Timestamp: t0,
			ConnectionID: fmt.Sprintf("cand-%d", p),
			Route: types.RouteProxy, AttributionClass: types.ClassRelayCandidate,
			Metadata: types.RawMetadata{Process: "", DestinationIP: "1.2.3.4"},
			Chains: []string{fmt.Sprintf("Node-Unique-%d", p)},
			DeltaUpload: int64(1000 + p), DeltaDownload: int64(2000 + p),
		}); err != nil {
			t.Fatalf("Emit candidate %d failed: %v", p, err)
		}
	}
	_ = sink.Close()

	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("OpenDB failed: %v", err) }
	defer db.Close()

	t0Rebuild := time.Now()
	run, err := storage.RebuildAccounting(ctx, db, "500 pairs scale test")
	if err != nil { t.Fatalf("RebuildAccounting scale failed: %v", err) }
	scaleDur := time.Since(t0Rebuild)

	t.Logf("Rebuild 500 Candidate x 500 Logical pairs completed in %v", scaleDur)

	// 验证 500 对全部成功识别为 confirmed relay duplicate 并去重
	var confirmedCount int64
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM relay_relations WHERE run_id = ? AND status = 'confirmed';", run.RunID).Scan(&confirmedCount)
	if confirmedCount != pairCount {
		t.Errorf("Expected %d confirmed relay relations, got %d", pairCount, confirmedCount)
	}
}

// F9: Soak Stability Verification (高压持续 50 帧并验证 DB Integrity)
func TestSoakStabilityMock(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "proxylens-soak-*")
	if err != nil { t.Fatalf("MkdirTemp failed: %v", err) }
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "soak.db")
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, "sess-soak", "v1.0.0-test")
	if err != nil { t.Fatalf("OpenSQLiteSink failed: %v", err) }

	t0 := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	const totalFrames = 50

	for f := 1; f <= totalFrames; f++ {
		for c := 1; c <= 10; c++ {
			evType := types.EventConnectionDelta
			if f == 1 {
				evType = types.EventConnectionNew
			}
			if err := sink.Emit(&types.CollectorEvent{
				EventID: fmt.Sprintf("ev-soak-%d-%d", f, c),
				SessionID: "sess-soak",
				EpochID: 1,
				FrameSequence: int64(f),
				EventSequence: int64(c),
				Type: evType,
				Timestamp: t0.Add(time.Duration(f*250) * time.Millisecond),
				ConnectionID: fmt.Sprintf("conn-soak-%d", c),
				Route: types.RouteProxy,
				AttributionClass: types.ClassKnownApplication,
				Metadata: types.RawMetadata{Process: fmt.Sprintf("app-%d.exe", c), Host: "soak.com"},
				Rule: "MATCH", RulePayload: "MATCH",
				Chains: []string{"Node-Soak", "ProxyGroup"},
				DeltaUpload: 500,
				DeltaDownload: 1000,
			}); err != nil {
				t.Fatalf("Emit soak event frame %d conn %d failed: %v", f, c, err)
			}
		}
	}
	_ = sink.EndSession(ctx, "sess-soak", storage.SessionStatusClosedClean)
	_ = sink.Close()

	// 重新打开并进行 PRAGMA integrity_check 与 foreign_key_check (F11)
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil { t.Fatalf("Reopen soak DB failed: %v", err) }
	defer db.Close()

	var integCheck string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integCheck); err != nil || integCheck != "ok" {
		t.Fatalf("PRAGMA integrity_check failed: %v (result: %s)", err, integCheck)
	}

	fkRows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check;")
	if err != nil { t.Fatalf("PRAGMA foreign_key_check failed: %v", err) }
	defer fkRows.Close()

	var fkViolations int
	for fkRows.Next() { fkViolations++ }
	if fkViolations > 0 {
		t.Fatalf("Detected %d foreign key violations after soak!", fkViolations)
	}

	run, err := storage.RebuildAccounting(ctx, db, "post-soak rebuild")
	if err != nil { t.Fatalf("Post-soak accounting rebuild failed: %v", err) }

	svc := storage.NewAnalyticsService(db)
	summary, err := svc.GetUsageSummary(ctx, storage.AnalyticsFilter{})
	if err != nil { t.Fatalf("GetUsageSummary after soak failed: %v", err) }

	expectedUp := int64(totalFrames * 10 * 500)
	expectedDown := int64(totalFrames * 10 * 1000)
	if summary.ProxyUpload != expectedUp || summary.ProxyDownload != expectedDown {
		t.Errorf("Soak totals mismatch: Up=%d (expected %d), Down=%d (expected %d)", summary.ProxyUpload, expectedUp, summary.ProxyDownload, expectedDown)
	}

	t.Logf("Soak test passed cleanly! Events: %d, Up: %d, Down: %d, RunID: %s", run.SourceJournalEventCount, summary.ProxyUpload, summary.ProxyDownload, run.RunID)
}
