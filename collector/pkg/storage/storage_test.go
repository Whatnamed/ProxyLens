package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func createTestDB(t *testing.T) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "proxylens-storage-test-*")
	if err != nil {
		t.Fatalf("Failed to create tmp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	return dbPath, cleanup
}

func TestStorageFullLifecycleAndReopen(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-1", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Failed to open sqlite sink: %v", err)
	}

	ev1 := &types.CollectorEvent{
		EventID:         "ev-1",
		SessionID:       "sess-1",
		EpochID:         1,
		FrameSequence:   1,
		EventSequence:   1,
		Type:            types.EventConnectionBootstrap,
		Timestamp:       time.Now(),
		ConnectionID:    "conn-1",
		ObservedUploadCounter: 1000,
		ObservedDownloadCounter: 2000,
		Route:           types.RouteProxy,
		AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process:      "curl.exe",
			Host:         "example.com",
			Network:      "tcp",
			SpecialRules: "DIRECT",
		},
	}

	if err := sink.Emit(ev1); err != nil {
		t.Fatalf("Emit ev1 failed: %v", err)
	}

	ev2 := &types.CollectorEvent{
		EventID:         "ev-2",
		SessionID:       "sess-1",
		EpochID:         1,
		FrameSequence:   2,
		EventSequence:   1,
		Type:            types.EventConnectionDelta,
		Timestamp:       time.Now(),
		ConnectionID:    "conn-1",
		DeltaUpload:     500,
		DeltaDownload:   1500,
		ObservedUploadCounter: 1500,
		ObservedDownloadCounter: 3500,
		MonitoredCumulativeUpload: 500,
		MonitoredCumulativeDownload: 1500,
		Route:           types.RouteProxy,
		AttributionClass: types.ClassKnownApplication,
	}

	if err := sink.Emit(ev2); err != nil {
		t.Fatalf("Emit ev2 failed: %v", err)
	}

	if err := sink.EndSession(ctx, "sess-1", SessionStatusClosedClean); err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}
	_ = sink.Close()

	// 重新打开验证持久性与查询
	reopenedDB, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen db: %v", err)
	}
	defer reopenedDB.Close()

	qs := NewQueryService(reopenedDB)
	conn, err := qs.GetConnection(ctx, "sess-1", 1, "conn-1")
	if err != nil {
		t.Fatalf("GetConnection failed on reopened db: %v", err)
	}

	if conn.Metadata.Process != "curl.exe" || conn.Metadata.Host != "example.com" || conn.Metadata.SpecialRules != "DIRECT" {
		t.Errorf("Metadata mismatch: %+v", conn.Metadata)
	}
	if conn.MonitoredUploadTotal != 500 || conn.MonitoredDownloadTotal != 1500 {
		t.Errorf("Monitored total mismatch: Up %d, Down %d", conn.MonitoredUploadTotal, conn.MonitoredDownloadTotal)
	}

	traffic, err := qs.ListConnectionTraffic(ctx, "sess-1", 1, "conn-1")
	if err != nil {
		t.Fatalf("ListConnectionTraffic failed: %v", err)
	}
	if len(traffic) != 1 {
		t.Fatalf("Expected 1 traffic delta record, got %d", len(traffic))
	}
	if traffic[0].DeltaUpload != 500 || traffic[0].DeltaDownload != 1500 {
		t.Errorf("Traffic delta mismatch: %+v", traffic[0])
	}
}

func TestStorageNumericNormalizationAndRebuildExactness(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-rebuild-exact", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	t0 := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

	events := []*types.CollectorEvent{
		{
			EventID: "e-boot", SessionID: "sess-rebuild-exact", EpochID: 1, FrameSequence: 1, EventSequence: 1,
			Type: types.EventConnectionBootstrap, Timestamp: t0, ConnectionID: "c-exact",
			Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
			ObservedUploadCounter: 100, ObservedDownloadCounter: 200,
			Metadata: types.RawMetadata{Process: "curl.exe", SpecialRules: "RULE_SET_CUSTOM"},
		},
		{
			EventID: "e-gap-open", SessionID: "sess-rebuild-exact", EpochID: 1, FrameSequence: 2, EventSequence: 1,
			Type: types.EventMonitoringGapOpened, Timestamp: t0.Add(time.Second),
			AttributionInterval: []string{t0.Add(time.Second).Format(time.RFC3339Nano), ""},
			Details: map[string]any{"reason": "stream_stall"},
		},
		{
			EventID: "e-gap-close", SessionID: "sess-rebuild-exact", EpochID: 1, FrameSequence: 3, EventSequence: 1,
			Type: types.EventMonitoringGapClosed, Timestamp: t0.Add(3 * time.Second),
			Details: map[string]any{
				"actualGapMs":                 int64(2000),
				"globalGapUploadDelta":        int64(1024),
				"globalGapDownloadDelta":      int64(2048),
				"gapPhysicalDeltaUnavailable": false,
			},
		},
		{
			EventID: "e-residual", SessionID: "sess-rebuild-exact", EpochID: 1, FrameSequence: 4, EventSequence: 1,
			Type: types.EventSamplingResidual, Timestamp: t0.Add(4 * time.Second),
			Details: map[string]any{
				"globalUploadDelta":      int64(500),
				"globalDownloadDelta":    int64(1000),
				"uniqueObservedUpload":   int64(400),
				"uniqueObservedDownload": int64(800),
				"residualUpload":         int64(100),
				"residualDownload":       int64(200),
			},
		},
	}

	for _, ev := range events {
		if err := sink.Emit(ev); err != nil {
			t.Fatalf("Emit failed for %s: %v", ev.EventID, err)
		}
	}
	_ = sink.Close()

	// 1. 查询重建前的值
	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	qs := NewQueryService(db)
	beforeConn, err := qs.GetConnection(ctx, "sess-rebuild-exact", 1, "c-exact")
	if err != nil {
		t.Fatalf("GetConnection before failed: %v", err)
	}
	beforeGaps, err := qs.ListMonitoringGaps(ctx, nil, nil)
	if err != nil || len(beforeGaps) != 1 {
		t.Fatalf("ListGaps before failed: count=%d, err=%v", len(beforeGaps), err)
	}
	beforeRes, err := qs.ListDiagnosticResiduals(ctx, nil, nil)
	if err != nil || len(beforeRes) != 1 {
		t.Fatalf("ListResiduals before failed: count=%d, err=%v", len(beforeRes), err)
	}

	// 2. 执行 Rebuild
	if err := RebuildProjections(ctx, db); err != nil {
		t.Fatalf("RebuildProjections failed: %v", err)
	}

	// 3. 校验重建后 100% 一致
	afterConn, err := qs.GetConnection(ctx, "sess-rebuild-exact", 1, "c-exact")
	if err != nil {
		t.Fatalf("GetConnection after failed: %v", err)
	}
	if afterConn.Metadata.SpecialRules != beforeConn.Metadata.SpecialRules {
		t.Errorf("SpecialRules mismatch: before=%s, after=%s", beforeConn.Metadata.SpecialRules, afterConn.Metadata.SpecialRules)
	}

	afterGaps, err := qs.ListMonitoringGaps(ctx, nil, nil)
	if err != nil || len(afterGaps) != 1 {
		t.Fatalf("ListGaps after failed: %v", err)
	}
	if *afterGaps[0].DurationMs != *beforeGaps[0].DurationMs || *afterGaps[0].GlobalGapUploadDelta != *beforeGaps[0].GlobalGapUploadDelta {
		t.Errorf("Gap rebuild value mismatch: before=%+v, after=%+v", beforeGaps[0], afterGaps[0])
	}

	afterRes, err := qs.ListDiagnosticResiduals(ctx, nil, nil)
	if err != nil || len(afterRes) != 1 {
		t.Fatalf("ListResiduals after failed: %v", err)
	}
	if afterRes[0].ResidualUpload != beforeRes[0].ResidualUpload || afterRes[0].ResidualDownload != beforeRes[0].ResidualDownload {
		t.Errorf("Residual rebuild value mismatch: before=%+v, after=%+v", beforeRes[0], afterRes[0])
	}
}

func TestStorageGapClosedWithoutOpenGapFails(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-orphan-gap", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sink.Close()

	ev := &types.CollectorEvent{
		EventID:       "e-orphan-close",
		SessionID:     "sess-orphan-gap",
		EpochID:       1,
		FrameSequence: 1,
		EventSequence: 1,
		Type:          types.EventMonitoringGapClosed,
		Timestamp:     time.Now(),
		Details:       map[string]any{"actualGapMs": int64(1000)},
	}

	err = sink.Emit(ev)
	if err == nil {
		t.Fatalf("Expected ErrProjectionContractViolation on orphan GapClosed, got nil")
	}
	if !errors.Is(err, ErrProjectionContractViolation) {
		t.Errorf("Expected error to wrap ErrProjectionContractViolation, got: %v", err)
	}
}

func TestStorageDuplicateEventNoProgress(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-duplicate-prog", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sink.Close()

	t0 := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	ev := &types.CollectorEvent{
		EventID:       "ev-dup-p",
		SessionID:     "sess-duplicate-prog",
		EpochID:       1,
		FrameSequence: 1,
		EventSequence: 1,
		Type:          types.EventConnectionBootstrap,
		Timestamp:     t0,
		ConnectionID:  "c-p",
		Metadata:      types.RawMetadata{Process: "curl.exe"},
	}

	if err := sink.Emit(ev); err != nil {
		t.Fatalf("First emit failed: %v", err)
	}

	// 读取初次 session 记录
	var lastEventStr string
	var lastFrameSeq int64
	err = sink.db.QueryRow("SELECT last_event_at, last_frame_sequence FROM collector_sessions WHERE session_id = ?", "sess-duplicate-prog").Scan(&lastEventStr, &lastFrameSeq)
	if err != nil {
		t.Fatalf("Query session failed: %v", err)
	}

	// 再次 Emit 完全相同的 duplicate 事件
	if err := sink.Emit(ev); err != nil {
		t.Fatalf("Duplicate emit failed: %v", err)
	}

	var afterEventStr string
	var afterFrameSeq int64
	err = sink.db.QueryRow("SELECT last_event_at, last_frame_sequence FROM collector_sessions WHERE session_id = ?", "sess-duplicate-prog").Scan(&afterEventStr, &afterFrameSeq)
	if err != nil {
		t.Fatalf("Query session after failed: %v", err)
	}

	if afterEventStr != lastEventStr || afterFrameSeq != lastFrameSeq {
		t.Errorf("Duplicate event modified session progress! before=(%s, %d), after=(%s, %d)",
			lastEventStr, lastFrameSeq, afterEventStr, afterFrameSeq)
	}
}

func TestStorageNewerSchemaFailsClosed(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	// 1. 初始化 DB
	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}

	// 2. 模拟未来更高版本的 migration (版本 999)
	_, err = db.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (999, '999_future_schema.sql', '2026-08-25T00:00:00Z')")
	if err != nil {
		t.Fatalf("Failed to insert mock future migration: %v", err)
	}
	_ = db.Close()

	// 3. 当前程序尝试打开具有更高版本的 DB，必须 fail closed 拒绝启动
	_, err = OpenDB(ctx, dbPath)
	if err == nil {
		t.Fatalf("Expected OpenDB to fail closed on newer schema, got nil")
	}
}

func TestStorageUpdateNonExistentConnectionFails(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-nonexistent", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sink.Close()

	// 直接对不存在的连接发送 Delta
	ev := &types.CollectorEvent{
		EventID:       "ev-delta-ghost",
		SessionID:     "sess-nonexistent",
		EpochID:       1,
		FrameSequence: 1,
		EventSequence: 1,
		Type:          types.EventConnectionDelta,
		Timestamp:     time.Now(),
		ConnectionID:  "ghost-conn",
		DeltaUpload:   100,
	}

	err = sink.Emit(ev)
	if err == nil {
		t.Fatalf("Expected ErrProjectionContractViolation on non-existent connection update, got nil")
	}
	if !errors.Is(err, ErrProjectionContractViolation) {
		t.Errorf("Expected ErrProjectionContractViolation, got: %v", err)
	}
}

func TestStorageTrafficAuthoritativeOrdering(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-traffic-order", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sink.Close()

	evBoot := &types.CollectorEvent{
		EventID: "b-1", SessionID: "sess-traffic-order", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionBootstrap, Timestamp: time.Now(), ConnectionID: "c-order",
		Metadata: types.RawMetadata{Process: "curl.exe"},
	}
	if err := sink.Emit(evBoot); err != nil {
		t.Fatalf("Emit bootstrap failed: %v", err)
	}

	// 连续写入多条 Delta
	for f := int64(2); f <= 5; f++ {
		evDelta := &types.CollectorEvent{
			EventID: "d-" + string(rune('0'+f)), SessionID: "sess-traffic-order", EpochID: 1, FrameSequence: f, EventSequence: 1,
			Type: types.EventConnectionDelta, Timestamp: time.Now(), ConnectionID: "c-order",
			DeltaUpload: 100 * f,
		}
		if err := sink.Emit(evDelta); err != nil {
			t.Fatalf("Emit delta failed: %v", err)
		}
	}

	qs := NewQueryService(sink.db)
	traffic, err := qs.ListConnectionTraffic(ctx, "sess-traffic-order", 1, "c-order")
	if err != nil {
		t.Fatalf("ListConnectionTraffic failed: %v", err)
	}
	if len(traffic) != 4 {
		t.Fatalf("Expected 4 delta traffic records, got %d", len(traffic))
	}

	for i, tr := range traffic {
		expectedFrame := int64(i + 2)
		if tr.FrameSequence != expectedFrame {
			t.Errorf("Traffic ordering mismatch at index %d: expected frame %d, got %d", i, expectedFrame, tr.FrameSequence)
		}
	}
}

func TestStorageIdempotencyAndNoDoubleCount(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-idemp", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sink.Close()

	ev := &types.CollectorEvent{
		EventID:       "ev-idemp-1",
		SessionID:     "sess-idemp",
		EpochID:       1,
		FrameSequence: 1,
		EventSequence: 1,
		Type:          types.EventConnectionNew,
		Timestamp:     time.Now(),
		ConnectionID:  "conn-idemp",
		DeltaUpload:   100,
		DeltaDownload: 200,
		ObservedUploadCounter: 100,
		ObservedDownloadCounter: 200,
		MonitoredCumulativeUpload: 100,
		MonitoredCumulativeDownload: 200,
		Route:         types.RouteDirect,
		AttributionClass: types.ClassKnownApplication,
	}

	// 第一次写入
	if err := sink.Emit(ev); err != nil {
		t.Fatalf("First emit failed: %v", err)
	}

	// 第二次重复写入完全相同的事件 (Idempotent replay)
	if err := sink.Emit(ev); err != nil {
		t.Fatalf("Second emit (duplicate) failed: %v", err)
	}

	qs := NewQueryService(sink.db)
	traffic, err := qs.ListConnectionTraffic(ctx, "sess-idemp", 1, "conn-idemp")
	if err != nil {
		t.Fatalf("Query traffic failed: %v", err)
	}
	if len(traffic) != 1 {
		t.Fatalf("Expected exactly 1 traffic record due to idempotency, got %d", len(traffic))
	}

	conn, err := qs.GetConnection(ctx, "sess-idemp", 1, "conn-idemp")
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}
	if conn.MonitoredUploadTotal != 100 || conn.MonitoredDownloadTotal != 200 {
		t.Fatalf("Double counting detected! Monitored Up: %d, Down: %d", conn.MonitoredUploadTotal, conn.MonitoredDownloadTotal)
	}
}

func TestStorageEventIDCollision(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-collision", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer sink.Close()

	ev1 := &types.CollectorEvent{
		EventID:       "ev-same-id",
		SessionID:     "sess-collision",
		EpochID:       1,
		FrameSequence: 1,
		EventSequence: 1,
		Type:          types.EventConnectionBootstrap,
		Timestamp:     time.Now(),
		ConnectionID:  "conn-A",
	}
	if err := sink.Emit(ev1); err != nil {
		t.Fatalf("First emit failed: %v", err)
	}

	// 相同 EventID 但不同内容 (Collision)
	ev2 := &types.CollectorEvent{
		EventID:       "ev-same-id",
		SessionID:     "sess-collision",
		EpochID:       1,
		FrameSequence: 1,
		EventSequence: 1,
		Type:          types.EventConnectionNew,
		Timestamp:     time.Now(),
		ConnectionID:  "conn-B-DIFFERENT",
	}

	err = sink.Emit(ev2)
	if err == nil {
		t.Fatalf("Expected collision error on conflicting EventID payload, got nil")
	}
}

func TestStorageSessionInterruptedAndOfflineGap(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	// 1. 会话 1：启动但非正常关闭（不调用 EndSession）
	sink1, err := OpenSQLiteSink(ctx, dbPath, "sess-crash-1", "v1.0.0")
	if err != nil {
		t.Fatalf("Open sink1 failed: %v", err)
	}
	_ = sink1.Close() // 模拟崩溃关闭

	// 2. 会话 2：启动，应该自动识别 sess-crash-1 为 interrupted 并生成 offline gap
	sink2, err := OpenSQLiteSink(ctx, dbPath, "sess-crash-2", "v1.0.0")
	if err != nil {
		t.Fatalf("Open sink2 failed: %v", err)
	}
	defer sink2.Close()

	qs := NewQueryService(sink2.db)
	gaps, err := qs.ListMonitoringGaps(ctx, nil, nil)
	if err != nil {
		t.Fatalf("ListMonitoringGaps failed: %v", err)
	}

	var foundOfflineGap bool
	for _, g := range gaps {
		if g.Source == "collector_session_boundary" && g.Reason == "collector_unclean_shutdown_or_process_termination" {
			foundOfflineGap = true
		}
	}

	if !foundOfflineGap {
		t.Errorf("Expected offline boundary gap to be generated for interrupted session!")
	}
}

func TestStorageStateEngineIntegration(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-engine-int", "v1.0.0")
	if err != nil {
		t.Fatalf("Open sink failed: %v", err)
	}
	defer sink.Close()

	engine := state.NewStateEngine(state.EngineOptions{
		Sink:      sink,
		SessionID: "sess-engine-int",
	})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-24T12:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "conn-live-1",
					Upload:   1000,
					Download: 2000,
					Chains:   []string{"DIRECT"},
					Metadata: types.RawMetadata{
						Process: "flclash.exe",
						Host:    "google.com",
						Network: "tcp",
					},
				},
			},
		},
	}
	if err := engine.ProcessFrame(frame1); err != nil {
		t.Fatalf("ProcessFrame 1 failed: %v", err)
	}

	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-24T12:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1500, DownloadTotal: 3000,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "conn-live-1",
					Upload:   1500,
					Download: 3000,
					Chains:   []string{"DIRECT"},
					Metadata: types.RawMetadata{
						Process: "flclash.exe",
						Host:    "google.com",
						Network: "tcp",
					},
				},
			},
		},
	}
	if err := engine.ProcessFrame(frame2); err != nil {
		t.Fatalf("ProcessFrame 2 failed: %v", err)
	}

	qs := NewQueryService(sink.db)
	conns, err := qs.ListConnections(ctx, ConnectionFilter{Process: "flclash"})
	if err != nil {
		t.Fatalf("ListConnections failed: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("Expected 1 connection matching flclash, got %d", len(conns))
	}
	if conns[0].MonitoredUploadTotal != 500 || conns[0].MonitoredDownloadTotal != 1000 {
		t.Errorf("Monitored delta mismatch: Up %d, Down %d", conns[0].MonitoredUploadTotal, conns[0].MonitoredDownloadTotal)
	}
}
