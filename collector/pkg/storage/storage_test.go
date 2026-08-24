package storage

import (
	"context"
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
			Process: "curl.exe",
			Host:    "example.com",
			Network: "tcp",
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

	if conn.Metadata.Process != "curl.exe" || conn.Metadata.Host != "example.com" {
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

func TestStorageProjectionRebuild(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	sink, err := OpenSQLiteSink(ctx, dbPath, "sess-rebuild", "v1.0.0-test")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	events := []*types.CollectorEvent{
		{
			EventID: "r-ev-1", SessionID: "sess-rebuild", EpochID: 1, FrameSequence: 1, EventSequence: 1,
			Type: types.EventConnectionBootstrap, Timestamp: time.Now(), ConnectionID: "c-1",
			Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
			ObservedUploadCounter: 100, ObservedDownloadCounter: 200,
			Metadata: types.RawMetadata{Process: "test.exe"},
		},
		{
			EventID: "r-ev-2", SessionID: "sess-rebuild", EpochID: 1, FrameSequence: 2, EventSequence: 1,
			Type: types.EventConnectionDelta, Timestamp: time.Now(), ConnectionID: "c-1",
			Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
			DeltaUpload: 50, DeltaDownload: 80,
			ObservedUploadCounter: 150, ObservedDownloadCounter: 280,
			MonitoredCumulativeUpload: 50, MonitoredCumulativeDownload: 80,
		},
	}

	for _, ev := range events {
		if err := sink.Emit(ev); err != nil {
			t.Fatalf("Emit failed: %v", err)
		}
	}
	_ = sink.Close()

	// 打开数据库并执行 RebuildProjections
	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if err := RebuildProjections(ctx, db); err != nil {
		t.Fatalf("RebuildProjections failed: %v", err)
	}

	qs := NewQueryService(db)
	conn, err := qs.GetConnection(ctx, "sess-rebuild", 1, "c-1")
	if err != nil {
		t.Fatalf("GetConnection failed after rebuild: %v", err)
	}
	if conn.Metadata.Process != "test.exe" {
		t.Errorf("Process mismatch after rebuild: %s", conn.Metadata.Process)
	}
	if conn.MonitoredUploadTotal != 50 || conn.MonitoredDownloadTotal != 80 {
		t.Errorf("Monitored totals mismatch after rebuild: %d / %d", conn.MonitoredUploadTotal, conn.MonitoredDownloadTotal)
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
