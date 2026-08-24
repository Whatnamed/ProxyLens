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
	engine := NewStateEngine(EngineOptions{Sink: memSink, SessionID: "test-sess"})

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
					Metadata: types.RawMetadata{Process: "chrome.exe", Host: "example.com", Network: "tcp", Type: "HTTP"},
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
	if ev.MonitoredCumulativeUpload != 0 || ev.MonitoredCumulativeDownload != 0 {
		t.Errorf("Bootstrap monitored cumulative must be 0, got Up=%d Down=%d", ev.MonitoredCumulativeUpload, ev.MonitoredCumulativeDownload)
	}
	if ev.ObservedUploadCounter != 100000 || ev.ObservedDownloadCounter != 500000 {
		t.Errorf("Observed counter mismatch: Up=%d Down=%d", ev.ObservedUploadCounter, ev.ObservedDownloadCounter)
	}
	if !ev.PreexistingAtStart {
		t.Errorf("Expected PreexistingAtStart=true")
	}
	if ev.Metadata.Network != "tcp" || ev.Metadata.Type != "HTTP" {
		t.Errorf("Raw metadata lost in event: %+v", ev.Metadata)
	}
}

func TestSteadyStateNewIDAndUpdates(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink, SessionID: "test-sess"})

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
	var newEv, deltaEv, resEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionNew {
			newEv = ev
		} else if ev.Type == types.EventConnectionDelta {
			deltaEv = ev
		} else if ev.Type == types.EventSamplingResidual {
			resEv = ev
		}
	}

	if newEv == nil || newEv.DeltaUpload != 100 || newEv.MonitoredCumulativeUpload != 100 {
		t.Fatalf("Unexpected new event: %+v", newEv)
	}
	if deltaEv == nil || deltaEv.DeltaUpload != 200 || deltaEv.MonitoredCumulativeUpload != 300 {
		t.Fatalf("Unexpected delta event: %+v", deltaEv)
	}
	if resEv == nil {
		t.Fatalf("Expected sampling residual event")
	}
}

func TestDisappearanceHandling(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink, SessionID: "test-sess"})

	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "c1", Upload: 100, Download: 200, Metadata: types.RawMetadata{Process: "app.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000, Connections: []types.ConnectionSnapshot{},
		},
	})

	events := memSink.GetEvents()
	var disEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventConnectionDisappeared {
			disEv = ev
		}
	}
	if disEv == nil || !disEv.PossibleUnobservedTail {
		t.Fatalf("Expected EventConnectionDisappeared with PossibleUnobservedTail=true")
	}
	if engine.GetActiveConnectionsCount() != 0 {
		t.Fatalf("Active connections must be 0 after disappearance")
	}
}

func TestPerIDCounterRegressionProtection(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink, SessionID: "test-sess"})

	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000, Connections: []types.ConnectionSnapshot{},
		},
	})

	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1500, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "c1", Upload: 500, Download: 1000, Metadata: types.RawMetadata{Process: "app.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	// Per-ID counter regresses to 100
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:02.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1600, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "c1", Upload: 100, Download: 1000, Metadata: types.RawMetadata{Process: "app.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	events := memSink.GetEvents()
	var healthEv *types.CollectorEvent
	for _, ev := range events {
		if ev.Type == types.EventCollectorHealth && ev.Details["issue"] == "connection_counter_regression" {
			healthEv = ev
		}
	}
	if healthEv == nil {
		t.Fatalf("Expected health event for per-ID counter regression")
	}
}

func TestEpochResetDuringGapHandledSafely(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink, SessionID: "test-sess"})

	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 50000000, DownloadTotal: 100000000,
			Connections: []types.ConnectionSnapshot{
				{ID: "conn-old-1", Upload: 1000, Download: 2000, Metadata: types.RawMetadata{Process: "app.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
			},
		},
	})

	gapStart := time.Date(2026, 8, 21, 0, 0, 1, 0, time.UTC)
	_ = engine.ProcessIngestItem(&types.IngestItem{Kind: types.ItemGapOpened, Timestamp: gapStart})

	_ = engine.ProcessIngestItem(&types.IngestItem{
		Kind:      types.ItemFrame,
		Timestamp: time.Date(2026, 8, 21, 0, 0, 5, 0, time.UTC),
		Frame: &types.ConnectionSnapshotFrame{
			ReceivedAt: "2026-08-21T00:00:05.000Z",
			Frame: types.ConnectionSnapshotPayload{
				UploadTotal: 1000, DownloadTotal: 2000,
				Connections: []types.ConnectionSnapshot{
					{ID: "conn-new-after-restart", Upload: 50, Download: 100, Metadata: types.RawMetadata{Process: "curl.exe"}, Rule: "Match", Chains: []string{"DIRECT"}},
				},
			},
		},
	})

	events := memSink.GetEvents()
	var gapCloseEv *types.CollectorEvent
	var epochBreakEv *types.CollectorEvent
	var bootstrapEv *types.CollectorEvent

	for _, ev := range events {
		if ev.Type == types.EventMonitoringGapClosed {
			gapCloseEv = ev
		} else if ev.Type == types.EventCounterEpochBreak {
			epochBreakEv = ev
		} else if ev.Type == types.EventConnectionBootstrap && ev.ConnectionID == "conn-new-after-restart" {
			bootstrapEv = ev
		}
	}

	if gapCloseEv == nil || gapCloseEv.Details["gapPhysicalDeltaUnavailable"] != true {
		t.Fatalf("Gap close across epoch break must flag gapPhysicalDeltaUnavailable=true, got %+v", gapCloseEv)
	}
	if epochBreakEv == nil || epochBreakEv.EpochID != 1 {
		t.Fatalf("Expected EventCounterEpochBreak on gap epoch reset")
	}
	if bootstrapEv == nil || bootstrapEv.EpochID != 2 || bootstrapEv.DeltaUpload != 0 {
		t.Fatalf("Expected new bootstrap in Epoch 2 with delta=0, got %+v", bootstrapEv)
	}
}

func TestRelayPairingDeduplicationInAccounting(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := NewStateEngine(EngineOptions{Sink: memSink, SessionID: "test-sess"})

	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000, Connections: []types.ConnectionSnapshot{},
		},
	})

	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 11000, DownloadTotal: 22000,
			Connections: []types.ConnectionSnapshot{
				{
					ID:       "logical-app-1",
					Upload:   10000,
					Download: 20000,
					Metadata: types.RawMetadata{Process: "curl.exe"},
					Rule:     "ProxyRule",
					Chains:   []string{"Node-HK-01", "ProxyGroup", "TopGroup"},
				},
				{
					ID:       "relay-underlying-1",
					Upload:   10000,
					Download: 20000,
					Metadata: types.RawMetadata{Process: ""},
					Rule:     "",
					Chains:   []string{"Node-HK-01", "ProxyGroup"},
				},
			},
		},
	})

	events := memSink.GetEvents()
	var residualEv *types.CollectorEvent
	var relayNewEv *types.CollectorEvent

	for _, ev := range events {
		if ev.Type == types.EventSamplingResidual {
			residualEv = ev
		}
		if ev.Type == types.EventConnectionNew && ev.ConnectionID == "relay-underlying-1" {
			relayNewEv = ev
		}
	}

	if relayNewEv == nil || relayNewEv.AttributionClass != types.ClassConfirmedRelayDuplicate {
		t.Fatalf("Expected relay-underlying-1 to be marked as confirmed_relay_duplicate, got %+v", relayNewEv)
	}

	uniqueUp := residualEv.Details["uniqueObservedUpload"].(int64)
	residualUp := residualEv.Details["residualUpload"].(int64)
	if uniqueUp != 10000 || residualUp != 0 {
		t.Errorf("UniqueObservedUpload must be 10000, got %d, Residual=%d", uniqueUp, residualUp)
	}
}

func TestSinkFailureFailSafe(t *testing.T) {
	failingSink := &sink.FailingSink{
		FailAfterCount: 2,
	}
	engine := NewStateEngine(EngineOptions{Sink: failingSink, SessionID: "test-sess"})

	frame := &types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 1000, DownloadTotal: 2000,
			Connections: []types.ConnectionSnapshot{
				{ID: "c1", Upload: 10, Download: 20, Metadata: types.RawMetadata{Process: "a.exe"}, Rule: "r", Chains: []string{"DIRECT"}},
				{ID: "c2", Upload: 10, Download: 20, Metadata: types.RawMetadata{Process: "b.exe"}, Rule: "r", Chains: []string{"DIRECT"}},
				{ID: "c3", Upload: 10, Download: 20, Metadata: types.RawMetadata{Process: "c.exe"}, Rule: "r", Chains: []string{"DIRECT"}},
			},
		},
	}

	err := engine.ProcessFrame(frame)
	if err == nil {
		t.Fatalf("Expected StateEngine to fail closed when sink emits error, got nil")
	}
}

func TestRouteClassificationAndQualityFlags(t *testing.T) {
	if attribution.ClassifyRoute([]string{"DIRECT"}) != types.RouteDirect {
		t.Errorf("Expected DIRECT")
	}
	if attribution.ClassifyRoute([]string{"REJECT"}) != types.RouteReject {
		t.Errorf("Expected REJECT")
	}
	if attribution.ClassifyRoute([]string{"Node-A", "ProxyGroup"}) != types.RouteProxy {
		t.Errorf("Expected PROXY")
	}
	if attribution.ClassifyRoute([]string{}) != types.RouteUnknown {
		t.Errorf("Expected UNKNOWN")
	}

	rawMeta := types.RawMetadata{
		Process:       "",
		ProcessPath:   "",
		Host:          "",
		DestinationIP: "1.1.1.1",
	}
	flags := rawMeta.DeriveQualityFlags("", []string{})
	if !flags.MissingProcess || !flags.IPOnly || !flags.MissingRule || !flags.MissingChain {
		t.Errorf("Unexpected quality flags: %+v", flags)
	}
}
