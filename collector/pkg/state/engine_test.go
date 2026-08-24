package state

import (
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/attribution"
	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func TestColdBootstrapNoHistoricalBurst(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	frame := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal:   1000000,
			DownloadTotal: 5000000,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "conn-preexisting-1",
					Upload:   100000,
					Download: 500000,
					Metadata: types.RawMetadata{Process: "chrome.exe", Host: "example.com"},
					Rule:     "Match",
					Chains:   []string{"DIRECT"},
				},
			},
		},
	}

	if err := engine.ProcessFrame(frame); err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}

	events := memSink.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 bootstrap event, got %d", len(events))
	}
	ev := events[0]
	if ev.Type != types.EventConnectionBootstrap {
		t.Errorf("Expected EventConnectionBootstrap, got %s", ev.Type)
	}
	if ev.DeltaUpload != 0 || ev.DeltaDownload != 0 {
		t.Errorf("Bootstrap delta must be 0, got Up=%d Down=%d", ev.DeltaUpload, ev.DeltaDownload)
	}
	if !ev.PreexistingAtStart {
		t.Errorf("Expected PreexistingAtStart=true")
	}
	summary := memSink.GetSummary()
	if summary["attributedUpload"].(int64) != 0 || summary["attributedDownload"].(int64) != 0 {
		t.Errorf("Bootstrap attributed bytes must be 0, got %v", summary)
	}
}

func TestSteadyStateNewIDAndUpdates(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	// Frame 0: Bootstrap (empty)
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000, Connections: []types.ConnectionSnapshot{},
		},
	})

	// Frame 1: New connection appears in steady state
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1100, DownloadTotal: 2500,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "conn-steady-1",
					Upload:   100,
					Download: 500,
					Metadata: types.RawMetadata{Process: "curl.exe", Host: "example.com"},
					Rule:     "Match",
					Chains:   []string{"DIRECT"},
				},
			},
		},
	})

	// Frame 2: Connection continues with delta
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:02.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1300, DownloadTotal: 3200,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "conn-steady-1",
					Upload:   300,
					Download: 1200,
					Metadata: types.RawMetadata{Process: "curl.exe", Host: "example.com"},
					Rule:     "Match",
					Chains:   []string{"DIRECT"},
				},
			},
		},
	})

	events := memSink.GetEvents()
	// Event 0: New connection
	if events[0].Type != types.EventConnectionNew || events[0].DeltaUpload != 100 || events[0].DeltaDownload != 500 {
		t.Errorf("Unexpected Event 0: %+v", events[0])
	}
	// Event 2: Delta update (Event 1 was Residual)
	var deltaEvent *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionDelta {
			deltaEvent = ev
			break
		}
	}
	if deltaEvent == nil || deltaEvent.DeltaUpload != 200 || deltaEvent.DeltaDownload != 700 {
		t.Errorf("Unexpected delta event: %+v", deltaEvent)
	}

	summary := memSink.GetSummary()
	if summary["attributedUpload"].(int64) != 300 || summary["attributedDownload"].(int64) != 1200 {
		t.Errorf("Attributed bytes mismatch: Up=%v Down=%v", summary["attributedUpload"], summary["attributedDownload"])
	}
}

func TestDisappearanceHandling(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	// Frame 0: Bootstrap with conn-1
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 100, Download: 200, Metadata: types.RawMetadata{Process: "chrome.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	// Frame 1: conn-1 disappears
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000, Connections: []types.ConnectionSnapshot{},
		},
	})

	events := memSink.GetEvents()
	var disEvent *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionDisappeared {
			disEvent = ev
			break
		}
	}
	if disEvent == nil {
		t.Fatalf("Expected EventConnectionDisappeared")
	}
	if !disEvent.PossibleUnobservedTail {
		t.Errorf("Disappearance must have PossibleUnobservedTail=true")
	}
	if engine.GetActiveConnectionsCount() != 0 {
		t.Errorf("Active connections count must be 0 after disappearance")
	}
}

func TestCounterRegressionProtection(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	// Frame 0: Bootstrap
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000, Connections: []types.ConnectionSnapshot{},
		},
	})

	// Frame 1: conn-1 at Up=500
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1500, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 500, Download: 1000, Metadata: types.RawMetadata{Process: "chrome.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	// Frame 2: Per-connection counter regresses to Up=100 (anomaly)
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:02.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1600, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 100, Download: 1000, Metadata: types.RawMetadata{Process: "chrome.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	events := memSink.GetEvents()
	var healthEvent *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventCollectorHealth {
			healthEvent = ev
			break
		}
	}
	if healthEvent == nil {
		t.Fatalf("Expected EventCollectorHealth for counter regression")
	}
	if healthEvent.Details["issue"] != "connection_counter_regression" {
		t.Errorf("Expected issue connection_counter_regression, got %v", healthEvent.Details["issue"])
	}
}

func TestEpochBreakHandling(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	// Frame 0: Bootstrap
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 50000, DownloadTotal: 100000, Connections: []types.ConnectionSnapshot{},
		},
	})

	// Frame 1: Normal steady state
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 60000, DownloadTotal: 120000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-1", Upload: 1000, Download: 2000, Metadata: types.RawMetadata{Process: "chrome.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	// Frame 2: Kernel restarted -> global counters drop to 1000/2000 (Epoch Reset)
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:02.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-2", Upload: 100, Download: 200, Metadata: types.RawMetadata{Process: "curl.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	events := memSink.GetEvents()
	var epochEvent *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventCounterEpochBreak {
			epochEvent = ev
			break
		}
	}
	if epochEvent == nil {
		t.Fatalf("Expected EventCounterEpochBreak on global counter drop")
	}
	summary := memSink.GetSummary()
	if summary["epochBreaksCount"].(int64) != 1 {
		t.Errorf("Expected 1 epoch break in summary")
	}
}

func TestGapRecoverySemantics(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink})

	// 1. Initial healthy frame
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-surviving-1", Upload: 100, Download: 200, Metadata: types.RawMetadata{Process: "chrome.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	// 2. Disconnection / Gap occurs
	gapStart := time.Date(2026, 8, 21, 0, 0, 1, 0, time.UTC)
	engine.MarkGapOpened(gapStart)

	// 3. Post-gap recovery first frame (4 seconds later)
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:05.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 5000, DownloadTotal: 10000,
			Connections: []types.ConnectionSnapshot{
				// Surviving connection: delta Up 400, Down 800
				{ID: "conn-surviving-1", Upload: 500, Download: 1000, Metadata: types.RawMetadata{Process: "chrome.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
				// New connection started inside gap: 1000/2000
				{ID: "conn-new-in-gap", Upload: 1000, Download: 2000, Start: "2026-08-21T00:00:03.000Z", Metadata: types.RawMetadata{Process: "curl.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	events := memSink.GetEvents()
	var gapEvent *types.CollectorEvent
	var survivingDeltaEvent *types.CollectorEvent
	var newInGapEvent *types.CollectorEvent

	for _, ev := range events {
		if ev.Type == types.EventMonitoringGapClosed {
			gapEvent = ev
		}
		if ev.Type == types.EventConnectionDelta && ev.ConnectionID == "conn-surviving-1" {
			survivingDeltaEvent = ev
		}
		if ev.Type == types.EventConnectionBootstrap && ev.ConnectionID == "conn-new-in-gap" {
			newInGapEvent = ev
		}
	}

	if gapEvent == nil {
		t.Fatalf("Expected EventMonitoringGapClosed")
	}
	if survivingDeltaEvent == nil || survivingDeltaEvent.DeltaUpload != 400 || survivingDeltaEvent.DeltaDownload != 800 {
		t.Errorf("Unexpected surviving delta: %+v", survivingDeltaEvent)
	}
	if survivingDeltaEvent.Precision != "interval_only" {
		t.Errorf("Surviving gap delta must have Precision=interval_only")
	}
	if newInGapEvent == nil || newInGapEvent.DeltaUpload != 0 {
		t.Errorf("New connection in post-gap frame must baseline delta=0 to avoid explosion, got %+v", newInGapEvent)
	}
}

func TestRouteClassificationAndRelayPairing(t *testing.T) {
	// Route classification
	if attribution.ClassifyRoute([]string{"DIRECT"}) != types.RouteDirect {
		t.Errorf("Expected DIRECT")
	}
	if attribution.ClassifyRoute([]string{"REJECT"}) != types.RouteReject {
		t.Errorf("Expected REJECT")
	}
	if attribution.ClassifyRoute([]string{"Node-A", "Proxy-Group"}) != types.RouteProxy {
		t.Errorf("Expected PROXY")
	}
	if attribution.ClassifyRoute([]string{}) != types.RouteUnknown {
		t.Errorf("Expected UNKNOWN")
	}

	// Relay Pairing
	cand := &types.ConnectionSnapshot{
		ID:       "relay-cand-1",
		Upload:   5000,
		Download: 10000,
		Metadata: types.RawMetadata{Process: ""},
		Rule:     "",
		Chains:   []string{"Node-A", "Proxy-Group"},
	}
	logical := &types.ConnectionSnapshot{
		ID:       "logical-app-1",
		Upload:   5000,
		Download: 10000,
		Metadata: types.RawMetadata{Process: "curl.exe"},
		Rule:     "Match",
		Chains:   []string{"Node-A", "Proxy-Group", "Top-Rule-Group"},
	}

	matched, evidence := attribution.CheckStructuralRelayPair(cand, logical, 5000, 10000, 5000, 10000)
	if !matched || evidence == nil {
		t.Fatalf("Expected confirmed structural relay pair")
	}

	// Structural False Positive: Completely different egress node
	unrelatedCand := &types.ConnectionSnapshot{
		ID:       "unrelated-cand",
		Upload:   5000,
		Download: 10000,
		Metadata: types.RawMetadata{Process: ""},
		Rule:     "",
		Chains:   []string{"Unrelated-Node-Z"},
	}
	m2, _ := attribution.CheckStructuralRelayPair(unrelatedCand, logical, 5000, 10000, 5000, 10000)
	if m2 {
		t.Errorf("Unrelated egress node must NOT match as relay pair")
	}
}
