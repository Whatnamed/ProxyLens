package state

import (
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
					Rule: "Proxy", Chains: []string{"Node-1"},
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
					Rule: "Proxy", Chains: []string{"Node-1"},
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
					Rule: "ProxyRule", Chains: []string{"Node-US-01", "ProxyGroup", "TopGroup"},
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
					Rule: "ProxyRule", Chains: []string{"Node-US-01", "ProxyGroup", "TopGroup"},
				},
				{
					ID: "logical-app-2", Upload: 5000, Download: 10000,
					Metadata: types.RawMetadata{Process: "firefox.exe", Host: "vimeo.com"},
					Rule: "ProxyRule", Chains: []string{"Node-US-01", "ProxyGroup", "TopGroup"},
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
