package state

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func TestColdBootstrapNoHistoricalBurst(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   5000000,
			DownloadTotal: 10000000,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "preexisting-1",
					Upload:   1000000,
					Download: 2000000,
					Metadata: types.RawMetadata{Process: "git.exe", Host: "github.com"},
					Rule:     "Proxy",
					Chains:   []string{"Node-1", "ProxyGroup"},
				},
			},
		},
	}

	if err := engine.ProcessFrame(frame1); err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}

	events := memSink.GetEvents()
	var bootstrapEvent *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionBootstrap && ev.ConnectionID == "preexisting-1" {
			bootstrapEvent = ev
			break
		}
	}

	if bootstrapEvent == nil {
		t.Fatalf("Expected ConnectionBootstrap event for preexisting-1")
	}

	if bootstrapEvent.DeltaUpload != 0 || bootstrapEvent.DeltaDownload != 0 {
		t.Errorf("Bootstrap delta must be 0, got Up:%d Down:%d", bootstrapEvent.DeltaUpload, bootstrapEvent.DeltaDownload)
	}

	if bootstrapEvent.MonitoredCumulativeUpload != 0 || bootstrapEvent.MonitoredCumulativeDownload != 0 {
		t.Errorf("Bootstrap monitored cumulative must be 0, got Up:%d Down:%d",
			bootstrapEvent.MonitoredCumulativeUpload, bootstrapEvent.MonitoredCumulativeDownload)
	}

	if !bootstrapEvent.PreexistingAtStart {
		t.Errorf("Expected PreexistingAtStart to be true")
	}
}

func TestSteadyStateNewIDAndUpdates(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{},
		},
	}
	_ = engine.ProcessFrame(frame1)

	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 2000, DownloadTotal: 4000,
			Connections: []types.ConnectionSnapshot{
				{
					ID: "conn-new-1", Upload: 500, Download: 1500,
					Metadata: types.RawMetadata{Process: "curl.exe", Host: "example.com"},
					Rule:     "Proxy", Chains: []string{"Node-1"},
				},
			},
		},
	}
	_ = engine.ProcessFrame(frame2)

	events := memSink.GetEvents()
	var newEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionNew && ev.ConnectionID == "conn-new-1" {
			newEv = ev
			break
		}
	}
	if newEv == nil {
		t.Fatalf("Expected ConnectionNew event for conn-new-1")
	}
	if newEv.DeltaUpload != 500 || newEv.DeltaDownload != 1500 {
		t.Errorf("Expected Delta 500/1500, got %d/%d", newEv.DeltaUpload, newEv.DeltaDownload)
	}

	frame3 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:02.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 3000, DownloadTotal: 6000,
			Connections: []types.ConnectionSnapshot{
				{
					ID: "conn-new-1", Upload: 700, Download: 2500,
					Metadata: types.RawMetadata{Process: "curl.exe", Host: "example.com"},
					Rule:     "Proxy", Chains: []string{"Node-1"},
				},
			},
		},
	}
	_ = engine.ProcessFrame(frame3)

	events = memSink.GetEvents()
	var deltaEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionDelta && ev.ConnectionID == "conn-new-1" && ev.FrameSequence == 3 {
			deltaEv = ev
			break
		}
	}
	if deltaEv == nil {
		t.Fatalf("Expected ConnectionDelta event for conn-new-1 on Frame 3")
	}
	if deltaEv.DeltaUpload != 200 || deltaEv.DeltaDownload != 1000 {
		t.Errorf("Expected Delta 200/1000, got %d/%d", deltaEv.DeltaUpload, deltaEv.DeltaDownload)
	}
	if deltaEv.MonitoredCumulativeUpload != 700 || deltaEv.MonitoredCumulativeDownload != 2500 {
		t.Errorf("Expected Monitored 700/2500, got %d/%d", deltaEv.MonitoredCumulativeUpload, deltaEv.MonitoredCumulativeDownload)
	}
}

func TestDisappearanceHandling(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 100, Download: 100, Chains: []string{"DIRECT"}},
			},
		},
	}
	_ = engine.ProcessFrame(frame1)

	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{},
		},
	}
	_ = engine.ProcessFrame(frame2)

	events := memSink.GetEvents()
	var disEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionDisappeared && ev.ConnectionID == "conn-1" {
			disEv = ev
			break
		}
	}
	if disEv == nil {
		t.Fatalf("Expected ConnectionDisappeared event")
	}
	if !disEv.PossibleUnobservedTail {
		t.Errorf("Expected PossibleUnobservedTail to be true")
	}
	if engine.GetActiveConnectionsCount() != 0 {
		t.Errorf("Expected 0 active connections, got %d", engine.GetActiveConnectionsCount())
	}
}

func TestPerIDCounterRegressionProtection(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 500, Download: 500, Chains: []string{"DIRECT"}},
			},
		},
	}
	_ = engine.ProcessFrame(frame1)

	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 1000, Download: 1000, Chains: []string{"DIRECT"}},
			},
		},
	}
	_ = engine.ProcessFrame(frame2)

	frame3 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:02.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 200, Download: 200, Chains: []string{"DIRECT"}},
			},
		},
	}
	_ = engine.ProcessFrame(frame3)

	events := memSink.GetEvents()
	var healthEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventCollectorHealth && ev.ConnectionID == "conn-1" {
			healthEv = ev
			break
		}
	}
	if healthEv == nil {
		t.Fatalf("Expected CollectorHealth for counter regression")
	}
	if healthEv.Details["issue"] != "connection_counter_regression" {
		t.Errorf("Expected issue connection_counter_regression, got %v", healthEv.Details["issue"])
	}
}

func TestEpochResetDuringGapHandledSafely(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 100000, DownloadTotal: 200000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 5000, Download: 10000, Chains: []string{"DIRECT"}},
			},
		},
	}
	_ = engine.ProcessFrame(frame1)

	_ = engine.ProcessIngestItem(&types.IngestItem{
		Kind:      types.ItemGapOpened,
		Timestamp: time.Now(),
	})

	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:10.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 500, DownloadTotal: 1000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 100, Download: 200, Chains: []string{"DIRECT"}},
			},
		},
	}
	_ = engine.ProcessFrame(frame2)

	events := memSink.GetEvents()
	var gapClosedEv *types.CollectorEvent
	var epochBreakEv *types.CollectorEvent
	var postBootstrapEv *types.CollectorEvent

	for _, ev := range events {
		if ev.Type == types.EventMonitoringGapClosed {
			gapClosedEv = ev
		} else if ev.Type == types.EventCounterEpochBreak {
			epochBreakEv = ev
		} else if ev.Type == types.EventConnectionBootstrap && ev.EpochID == 2 {
			postBootstrapEv = ev
		}
	}

	if gapClosedEv == nil {
		t.Fatalf("Expected MonitoringGapClosed event")
	}
	if gapClosedEv.Details["gapPhysicalDeltaUnavailable"] != true {
		t.Errorf("Expected gapPhysicalDeltaUnavailable to be true on epoch break across gap")
	}

	if epochBreakEv == nil {
		t.Fatalf("Expected CounterEpochBreak event")
	}
	if postBootstrapEv == nil {
		t.Fatalf("Expected post-epoch ConnectionBootstrap event")
	}
	if postBootstrapEv.DeltaUpload != 0 || postBootstrapEv.DeltaDownload != 0 {
		t.Errorf("Expected post-epoch bootstrap delta to be 0, got %d/%d", postBootstrapEv.DeltaUpload, postBootstrapEv.DeltaDownload)
	}
}

func TestRelayPairingDeduplicationInAccounting(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 10000, DownloadTotal: 20000,
			Connections: []types.ConnectionSnapshot{},
		},
	}
	_ = engine.ProcessFrame(frame1)

	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 15000, DownloadTotal: 30000,
			Connections: []types.ConnectionSnapshot{
				{
					ID: "logical-app-1", Upload: 5000, Download: 10000,
					Metadata: types.RawMetadata{Process: "chrome.exe", Host: "youtube.com"},
					Rule:     "ProxyRule", Chains: []string{"Node-US-01", "ProxyGroup", "TopGroup"},
				},
				{
					ID: "relay-cand-1", Upload: 5000, Download: 10000,
					Metadata: types.RawMetadata{},
					Chains:   []string{"Node-US-01", "ProxyGroup"},
				},
			},
		},
	}
	_ = engine.ProcessFrame(frame2)

	events := memSink.GetEvents()
	var residualEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventSamplingResidual && ev.FrameSequence == 2 {
			residualEv = ev
			break
		}
	}

	if residualEv == nil {
		t.Fatalf("Expected SamplingResidual event for frame 2")
	}

	details := residualEv.Details
	uniqueUp := details["uniqueObservedUpload"].(int64)
	uniqueDown := details["uniqueObservedDownload"].(int64)
	residualUp := details["residualUpload"].(int64)
	residualDown := details["residualDownload"].(int64)

	if uniqueUp != 5000 || uniqueDown != 10000 {
		t.Errorf("Expected UniqueObserved to be 5000/10000 (excluding relay duplicate), got %d/%d", uniqueUp, uniqueDown)
	}

	if residualUp != 0 || residualDown != 0 {
		t.Errorf("Expected Residual to be 0/0, got %d/%d", residualUp, residualDown)
	}
}

func TestMetadataEvolutionImprovement(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{
				{
					ID: "conn-1", Upload: 100, Download: 100,
					Metadata: types.RawMetadata{DestinationIP: "1.1.1.1"},
					Chains:   []string{"DIRECT"},
				},
			},
		},
	}
	_ = engine.ProcessFrame(frame1)

	// 第二帧：Mihomo 补齐了 Host 和 Process
	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{
				{
					ID: "conn-1", Upload: 200, Download: 200,
					Metadata: types.RawMetadata{
						DestinationIP: "1.1.1.1",
						Host:          "one.one.one.one",
						Process:       "dns.exe",
					},
					Rule:   "Match",
					Chains: []string{"DIRECT"},
				},
			},
		},
	}
	_ = engine.ProcessFrame(frame2)

	events := memSink.GetEvents()
	var updateEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionMetadataUpdated && ev.ConnectionID == "conn-1" {
			updateEv = ev
			break
		}
	}

	if updateEv == nil {
		t.Fatalf("Expected ConnectionMetadataUpdated event when metadata improved")
	}
	if updateEv.Metadata.Host != "one.one.one.one" || updateEv.Metadata.Process != "dns.exe" {
		t.Errorf("Expected improved metadata in update event, got %+v", updateEv.Metadata)
	}
}

func TestIPOnlyWithSniffHost(t *testing.T) {
	raw := types.RawMetadata{
		DestinationIP: "1.1.1.1",
		SniffHost:     "example.com",
	}
	flags := raw.DeriveQualityFlags("Match", []string{"DIRECT"})
	if flags.IPOnly {
		t.Errorf("Connection with SniffHost must NOT be marked as IPOnly")
	}
	if !flags.MissingHost {
		t.Errorf("MissingHost should still be true since raw Host is empty")
	}
}

func TestRelayAmbiguousNoDedup(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 10000, DownloadTotal: 20000,
			Connections: []types.ConnectionSnapshot{},
		},
	}
	_ = engine.ProcessFrame(frame1)

	// 1 个 candidate 匹配 2 个相同的 logical (歧义)
	frame2 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 25000, DownloadTotal: 50000,
			Connections: []types.ConnectionSnapshot{
				{
					ID: "logical-app-1", Upload: 5000, Download: 10000,
					Metadata: types.RawMetadata{Process: "chrome.exe", Host: "youtube.com"},
					Rule:     "ProxyRule", Chains: []string{"Node-US-01", "ProxyGroup", "TopGroup"},
				},
				{
					ID: "logical-app-2", Upload: 5000, Download: 10000,
					Metadata: types.RawMetadata{Process: "firefox.exe", Host: "vimeo.com"},
					Rule:     "ProxyRule", Chains: []string{"Node-US-01", "ProxyGroup", "TopGroup"},
				},
				{
					ID: "relay-cand-1", Upload: 5000, Download: 10000,
					Metadata: types.RawMetadata{},
					Chains:   []string{"Node-US-01", "ProxyGroup"},
				},
			},
		},
	}
	_ = engine.ProcessFrame(frame2)

	events := memSink.GetEvents()
	var residualEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventSamplingResidual && ev.FrameSequence == 2 {
			residualEv = ev
			break
		}
	}
	if residualEv == nil {
		t.Fatalf("Expected SamplingResidual event")
	}

	// 歧义时不自动去重：uniqueObserved 包含全部 3 个连接 = 15000 / 30000
	uniqueUp := residualEv.Details["uniqueObservedUpload"].(int64)
	if uniqueUp != 15000 {
		t.Errorf("Expected ambiguous candidate NOT to be deduplicated (uniqueObservedUpload=15000), got %d", uniqueUp)
	}
}

func TestSinkFailureFailSafe(t *testing.T) {
	failingSink := &sink.FailingSink{FailAfterCount: 3}
	engine := NewStateEngine(EngineOptions{Sink: failingSink})

	frame1 := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			Connections: []types.ConnectionSnapshot{
				{ID: "c1", Upload: 10, Download: 10, Chains: []string{"DIRECT"}},
				{ID: "c2", Upload: 20, Download: 20, Chains: []string{"DIRECT"}},
				{ID: "c3", Upload: 30, Download: 30, Chains: []string{"DIRECT"}},
			},
		},
	}

	err := engine.ProcessFrame(frame1)
	if err == nil {
		t.Fatalf("Expected error when sink fails, got nil")
	}
}

func TestFirstHealthyFrameRecordsControllerUnreachableGap(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})
	// Simulate a session that started unable to reach the controller.
	engine.createdAt = time.Now().Add(-5 * time.Minute)

	frame := &types.ConnectionSnapshotFrame{
		ReceivedAt: time.Now().Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1000,
			DownloadTotal: 2000,
			Connections:   []types.ConnectionSnapshot{},
		},
	}
	if err := engine.ProcessFrame(frame); err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}

	var opened, closed *types.CollectorEvent
	for _, ev := range memSink.GetEvents() {
		switch ev.Type {
		case types.EventMonitoringGapOpened:
			if opened == nil {
				opened = ev
			}
		case types.EventMonitoringGapClosed:
			if closed == nil {
				closed = ev
			}
		}
	}
	if opened == nil || closed == nil {
		t.Fatalf("expected controller_stream gap pair after first healthy frame, got opened=%v closed=%v", opened != nil, closed != nil)
	}
	if len(opened.AttributionInterval) != 1 || len(closed.AttributionInterval) != 2 {
		t.Fatalf("gap interval mismatch: opened=%v closed=%v", opened.AttributionInterval, closed.AttributionInterval)
	}
	gapEnd, err := time.Parse(time.RFC3339Nano, closed.AttributionInterval[1])
	if err != nil {
		t.Fatalf("gap end %q is not RFC3339: %v", closed.AttributionInterval[1], err)
	}
	frameTime, err := time.Parse(time.RFC3339Nano, frame.ReceivedAt)
	if err != nil {
		t.Fatalf("frame time %q is not RFC3339: %v", frame.ReceivedAt, err)
	}
	if !gapEnd.Equal(frameTime) {
		t.Fatalf("gap end %q must equal first healthy frame time %q", closed.AttributionInterval[1], frame.ReceivedAt)
	}
	if !strings.HasSuffix(closed.AttributionInterval[1], "Z") {
		t.Fatalf("gap bounds must be stored UTC, got %q", closed.AttributionInterval[1])
	}
	if opened.Details["reason"] != controllerUnreachableGapReason {
		t.Fatalf("unexpected gap reason: %v", opened.Details["reason"])
	}
}

func TestImmediateBootstrapSkipsUnreachableGap(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})
	engine.createdAt = time.Now().Add(-100 * time.Millisecond)

	frame := &types.ConnectionSnapshotFrame{
		ReceivedAt: time.Now().Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1000,
			DownloadTotal: 2000,
			Connections:   []types.ConnectionSnapshot{},
		},
	}
	if err := engine.ProcessFrame(frame); err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}
	for _, ev := range memSink.GetEvents() {
		if ev.Type == types.EventMonitoringGapOpened || ev.Type == types.EventMonitoringGapClosed {
			t.Fatalf("sub-second bootstrap must not record a controller gap, got %s", ev.Type)
		}
	}
}

func TestEmitUnobservedSessionGap(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})
	engine.createdAt = time.Now().Add(-3 * time.Minute)

	if err := engine.EmitUnobservedSessionGap(time.Now()); err != nil {
		t.Fatalf("EmitUnobservedSessionGap failed: %v", err)
	}
	var opened, closed int
	for _, ev := range memSink.GetEvents() {
		if ev.Type == types.EventMonitoringGapOpened {
			opened++
		}
		if ev.Type == types.EventMonitoringGapClosed {
			closed++
		}
	}
	if opened != 1 || closed != 1 {
		t.Fatalf("expected exactly one gap pair, opened=%d closed=%d", opened, closed)
	}

	// Second call must not duplicate the gap.
	if err := engine.EmitUnobservedSessionGap(time.Now()); err != nil {
		t.Fatalf("second EmitUnobservedSessionGap failed: %v", err)
	}
	opened, closed = 0, 0
	for _, ev := range memSink.GetEvents() {
		if ev.Type == types.EventMonitoringGapOpened {
			opened++
		}
		if ev.Type == types.EventMonitoringGapClosed {
			closed++
		}
	}
	if opened != 1 || closed != 1 {
		t.Fatalf("gap pair duplicated on second call: opened=%d closed=%d", opened, closed)
	}

	// A healthy session must not record the gap.
	healthySink := sink.NewMemorySink()
	healthyEngine := NewStateEngine(EngineOptions{Sink: healthySink})
	healthyEngine.createdAt = time.Now().Add(-100 * time.Millisecond)
	_ = healthyEngine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: time.Now().Format(time.RFC3339Nano),
		Frame:      types.ConnectionSnapshotPayload{Connections: []types.ConnectionSnapshot{}},
	})
	if err := healthyEngine.EmitUnobservedSessionGap(time.Now()); err != nil {
		t.Fatalf("EmitUnobservedSessionGap on healthy engine failed: %v", err)
	}
	for _, ev := range healthySink.GetEvents() {
		if ev.Type == types.EventMonitoringGapOpened {
			t.Fatalf("healthy session must not record unobserved gap")
		}
	}
}

// TestTombstoneFIFOChurnDoesNotEvictNewestTombstone verifies the versioned
// FIFO eviction contract: when a connection flaps multiple times, older FIFO
// entries for that ID being evicted by intervening churn (>4096 other
// tombstones) MUST NOT delete the latest tombstone for that ID. The final
// reappearance must resume with counter continuation (counter-diff), not
// full-count.
func TestTombstoneFIFOChurnDoesNotEvictNewestTombstone(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	const mihomoStart = "2026-09-06T10:00:00.000Z"
	baseTs := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

	// Frame 1: Bootstrap empty session.
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: baseTs.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1000,
			DownloadTotal: 2000,
			Connections:   []types.ConnectionSnapshot{},
		},
	}); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	// Frame 2: target-conn appears for the first time (300 / 600).
	ts2 := baseTs.Add(time.Second)
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts2.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1300,
			DownloadTotal: 2600,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "target-conn",
					Start:    mihomoStart,
					Upload:   300,
					Download: 600,
					Metadata: types.RawMetadata{Process: "app.exe", Host: "example.com"},
					Chains:   []string{"NodeA", "GroupX"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("frame 2 failed: %v", err)
	}

	// Frame 3: target-conn disappears (first flap -> tombstone 1 created, FIFO has target-conn).
	ts3 := baseTs.Add(2 * time.Second)
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts3.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1300,
			DownloadTotal: 2600,
			Connections:   []types.ConnectionSnapshot{},
		},
	}); err != nil {
		t.Fatalf("frame 3 failed: %v", err)
	}

	// Frame 4: target-conn reappears (350 / 650, same Start). Consumes tombstone 1 from map.
	// But the old FIFO entry pointing to tombstone 1 remains queued in the FIFO.
	ts4 := baseTs.Add(3 * time.Second)
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts4.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1350,
			DownloadTotal: 2650,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "target-conn",
					Start:    mihomoStart,
					Upload:   350,
					Download: 650,
					Metadata: types.RawMetadata{Process: "app.exe", Host: "example.com"},
					Chains:   []string{"NodeA", "GroupX"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("frame 4 failed: %v", err)
	}

	// Frame 5: Batch A of 4000 churn connections appears, while target-conn stays active.
	const churnA = 4000
	churnConnsA := make([]types.ConnectionSnapshot, churnA+1)
	churnConnsA[0] = types.ConnectionSnapshot{
		ID:       "target-conn",
		Start:    mihomoStart,
		Upload:   350,
		Download: 650,
		Metadata: types.RawMetadata{Process: "app.exe", Host: "example.com"},
		Chains:   []string{"NodeA", "GroupX"},
	}
	for i := range churnA {
		churnConnsA[i+1] = types.ConnectionSnapshot{
			ID:       fmt.Sprintf("churnA-%05d", i),
			Start:    mihomoStart,
			Upload:   10,
			Download: 20,
			Metadata: types.RawMetadata{Process: "churn.exe"},
			Chains:   []string{"DIRECT"},
		}
	}
	ts5 := baseTs.Add(4 * time.Second)
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts5.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1350 + int64(churnA*10),
			DownloadTotal: 2650 + int64(churnA*20),
			Connections:   churnConnsA,
		},
	}); err != nil {
		t.Fatalf("frame 5 failed: %v", err)
	}

	// Frame 6: target-conn AND all 4000 churn connections disappear!
	// target-conn produces tombstone 2.
	// FIFO now contains: [target-conn(tombstone 1), 4000 churn entries, target-conn(tombstone 2)].
	// Total FIFO length = 4002 <= 4096 (no eviction of tombstone 1 yet; tombstone 1 is at index 0).
	ts6 := baseTs.Add(5 * time.Second)
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts6.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1350 + int64(churnA*10),
			DownloadTotal: 2650 + int64(churnA*20),
			Connections:   []types.ConnectionSnapshot{},
		},
	}); err != nil {
		t.Fatalf("frame 6 failed: %v", err)
	}

	// Frame 7: Generate 200 more churn connections and disappear them.
	// Total churn in FIFO exceeds 4096, forcing eviction of the queue head.
	// The queue head is target-conn's stale tombstone 1!
	// In unversioned code, popping index 0 calls delete(map, "target-conn"), which erroneously
	// destroys tombstone 2. In versioned code, tombstone 1 does not match the map's current
	// tombstone 2, so tombstone 2 is preserved!
	const churnB = 200
	churnConnsB := make([]types.ConnectionSnapshot, churnB)
	for i := range churnB {
		churnConnsB[i] = types.ConnectionSnapshot{
			ID:       fmt.Sprintf("churnB-%05d", i),
			Start:    mihomoStart,
			Upload:   10,
			Download: 20,
			Metadata: types.RawMetadata{Process: "churn.exe"},
			Chains:   []string{"DIRECT"},
		}
	}
	ts7a := baseTs.Add(6 * time.Second)
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts7a.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1350 + int64((churnA+churnB)*10),
			DownloadTotal: 2650 + int64((churnA+churnB)*20),
			Connections:   churnConnsB,
		},
	}); err != nil {
		t.Fatalf("frame 7a failed: %v", err)
	}
	ts7b := baseTs.Add(7 * time.Second)
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts7b.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1350 + int64((churnA+churnB)*10),
			DownloadTotal: 2650 + int64((churnA+churnB)*20),
			Connections:   []types.ConnectionSnapshot{},
		},
	}); err != nil {
		t.Fatalf("frame 7b failed: %v", err)
	}

	// Check: target-conn's tombstone 2 MUST still exist in engine memory!
	engine.mu.Lock()
	tomb, exists := engine.disappearedTombstones["target-conn"]
	engine.mu.Unlock()
	if !exists || tomb == nil {
		t.Fatalf("target-conn newest tombstone was erroneously evicted by stale FIFO entry!")
	}

	// Frame 8: target-conn reappears (400 / 700, same Start).
	// It MUST be treated as counter continuation (+50 / +50), NEVER as full count (400 / 700)!
	ts8 := baseTs.Add(8 * time.Second)
	eventsBefore := len(memSink.GetEvents())
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: ts8.Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1400 + int64((churnA+churnB)*10),
			DownloadTotal: 2700 + int64((churnA+churnB)*20),
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "target-conn",
					Start:    mihomoStart,
					Upload:   400,
					Download: 700,
					Metadata: types.RawMetadata{Process: "app.exe", Host: "example.com"},
					Chains:   []string{"NodeA", "GroupX"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("final reappearance frame failed: %v", err)
	}

	// Find the ConnectionNew event for target-conn.
	var reobsEv *types.CollectorEvent
	for _, ev := range memSink.GetEvents()[eventsBefore:] {
		if ev.ConnectionID == "target-conn" && ev.Type == types.EventConnectionNew {
			reobsEv = ev
			break
		}
	}
	if reobsEv == nil {
		t.Fatalf("expected ConnectionNew reobservation event for target-conn")
	}

	// Delta MUST be counter-diff (400 - 350 = 50, 700 - 650 = 50).
	if reobsEv.DeltaUpload != 50 || reobsEv.DeltaDownload != 50 {
		t.Fatalf("expected counter-diff delta 50/50, got deltaUp=%d deltaDown=%d (full-count regression!)",
			reobsEv.DeltaUpload, reobsEv.DeltaDownload)
	}
	// Cumulative monitored totals must be 400 / 700.
	if reobsEv.MonitoredCumulativeUpload != 400 || reobsEv.MonitoredCumulativeDownload != 700 {
		t.Fatalf("expected cumulative totals 400/700, got up=%d down=%d",
			reobsEv.MonitoredCumulativeUpload, reobsEv.MonitoredCumulativeDownload)
	}
}
