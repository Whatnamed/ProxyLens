package state

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/attribution"
	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// ActiveConnectionState 维护单个存活连接的内部状态
type ActiveConnectionState struct {
	Snapshot                    types.ConnectionSnapshot
	FirstObservedAt             time.Time
	LastObservedAt              time.Time
	LastUploadCounter           int64
	LastDownloadCounter         int64
	MonitoredCumulativeUpload   int64
	MonitoredCumulativeDownload int64
	BaselineUploadCounter       int64
	BaselineDownloadCounter     int64
	// LastDurableEvidenceAt is the timestamp of the most recent durable
	// per-connection evidence emission (ConnectionNew, ConnectionDelta,
	// ConnectionPresenceCheckpoint, ConnectionMetadataUpdated, relay class
	// change or counter-regression health event). It drives the sparse
	// presence checkpoint cadence for connections without traffic.
	LastDurableEvidenceAt  time.Time
	PreexistingAtStart     bool
	PossibleUnobservedTail bool
	Route                  types.RouteType
	AttributionClass       types.AttributionClass
	QualityFlags           types.QualityFlags
}

// disappearedTombstone 保留一条刚从快照中消失的连接的完整观测状态，用于区分
// "同一个 Mihomo connection 短暂漏帧后重现" 与 "真正的新 incarnation"：
// 重现时以 connection ID + Mihomo Start 作为同一实际 connection 的证据。
type disappearedTombstone struct {
	state         *ActiveConnectionState
	disappearedAt time.Time
}

// reobservationContinuation is the per-frame continuation decision for a
// connection that reappeared with the same ID and the same Mihomo start.
type reobservationContinuation struct {
	tomb      *disappearedTombstone
	deltaUp   int64
	deltaDown int64
	regressed bool
}

// reobservationDetails builds the re-observation New event's evidence: the
// lifecycle continuation facts plus any frame relay-dedup evidence, flattened
// to match the New event evidence convention.
func reobservationDetails(tomb *disappearedTombstone, st *ActiveConnectionState, relayEvidence map[string]any) map[string]any {
	details := map[string]any{
		"reobservedAfterDisappearance": true,
		"sameMihomoStart":              true,
		"disappearedAt":                tomb.disappearedAt.UTC().Format(time.RFC3339Nano),
		"lastObservedAt":               st.LastObservedAt.UTC().Format(time.RFC3339Nano),
	}
	for k, v := range relayEvidence {
		details[k] = v
	}
	return details
}

// maxDisappearedTombstones bounds tombstone memory. Normal disappearance
// bursts are a fraction of the live set per frame; the cap only protects
// against pathological churn and evicts oldest-first (FIFO).
const maxDisappearedTombstones = 4096

// EngineOptions 配置状态机参数
type EngineOptions struct {
	Sink      sink.EventSink
	SessionID string
}

// StateEngine 是 Collector 的核心状态机实现
type StateEngine struct {
	mu                                      sync.Mutex
	sink                                    sink.EventSink
	sessionID                               string
	epochID                                 int
	frameSequence                           int64
	eventSequence                           int64
	sessionState                            types.SessionState
	activeMap                               map[string]*ActiveConnectionState
	disappearedTombstones                   map[string]*disappearedTombstone
	tombstoneFIFO                           []tombstoneFIFOEntry
	prevUploadTotal                         int64
	prevDownloadTotal                       int64
	hasEverBeenHealthy                      bool
	gapIsOpen                               bool
	gapStartTime                            time.Time
	gapStartTimeMonotonic                   time.Time
	gapStartTimeString                      string
	gapInjectionDetails                     map[string]any
	lastSuccessfullyProcessedHealthyFrameAt time.Time
	lastHealthyMonotonic                    time.Time
	lastSuccessfullyProcessedHealthyStr     string
	isBootstrapFrame                        bool
	createdAt                               time.Time
	unobservedGapEmitted                    bool
}

// NewStateEngine 创建 StateEngine 实例
func NewStateEngine(opts EngineOptions) *StateEngine {
	s := opts.Sink
	if s == nil {
		s = sink.NewMemorySink()
	}
	sessID := opts.SessionID
	if sessID == "" {
		sessID = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return &StateEngine{
		sink:                  s,
		sessionID:             sessID,
		epochID:               1,
		sessionState:          types.SessionStarting,
		activeMap:             make(map[string]*ActiveConnectionState),
		disappearedTombstones: make(map[string]*disappearedTombstone),
		isBootstrapFrame:      true,
		createdAt:             time.Now(),
	}
}

// resetTombstones clears disappeared-connection tracking. It must run on every
// counter epoch break: after a Mihomo counter reset the old epoch's
// connection history (including lifetimes) is a different observation keyspace.
func (e *StateEngine) resetTombstones() {
	e.disappearedTombstones = make(map[string]*disappearedTombstone)
	e.tombstoneFIFO = nil
}
type tombstoneFIFOEntry struct {
	id   string
	tomb *disappearedTombstone
}

// recordDisappearedTombstone preserves a connection's observation state at
// disappearance so a same-ID + same-Start reappearance can resume the
// lifecycle with counter-continuation semantics. A versioned/pointer-aware
// FIFO queue bounds memory without allowing stale FIFO entries to evict newer
// tombstones for the same ID.
func (e *StateEngine) recordDisappearedTombstone(id string, st *ActiveConnectionState, at time.Time) {
	tomb := &disappearedTombstone{state: st, disappearedAt: at}
	e.disappearedTombstones[id] = tomb
	e.tombstoneFIFO = append(e.tombstoneFIFO, tombstoneFIFOEntry{id: id, tomb: tomb})
	for len(e.tombstoneFIFO) > maxDisappearedTombstones {
		oldest := e.tombstoneFIFO[0]
		e.tombstoneFIFO = e.tombstoneFIFO[1:]
		if current, ok := e.disappearedTombstones[oldest.id]; ok && current == oldest.tomb {
			delete(e.disappearedTombstones, oldest.id)
		}
	}
}

// takeDisappearedTombstone consumes the tombstone of a re-observed connection.
func (e *StateEngine) takeDisappearedTombstone(id string) (*disappearedTombstone, bool) {
	t, ok := e.disappearedTombstones[id]
	if ok {
		delete(e.disappearedTombstones, id)
	}
	return t, ok
}

// emitEvent 统一通过 Sink 输出事件并生成唯一序列和确定性 ID，任何错误必须向上返回
func (e *StateEngine) emitEvent(event *types.CollectorEvent) error {
	e.eventSequence++
	event.SessionID = e.sessionID
	event.EpochID = e.epochID
	event.FrameSequence = e.frameSequence
	event.EventSequence = e.eventSequence
	event.GenerateDeterministicEventID()

	if err := e.sink.Emit(event); err != nil {
		return fmt.Errorf("sink emit failure on event %s (%s): %w", event.EventID, event.Type, err)
	}
	return nil
}

// EmitSessionHealth outputs a CollectorHealth event associated with the session,
// inheriting the engine's current frame and monotonic event sequence.
func (e *StateEngine) EmitSessionHealth(timestamp time.Time, issue, description string, details map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	evDetails := make(map[string]any)
	for k, v := range details {
		evDetails[k] = v
	}
	evDetails["issue"] = issue
	evDetails["description"] = description
	return e.emitEvent(&types.CollectorEvent{
		Type:      types.EventCollectorHealth,
		Timestamp: timestamp,
		Details:   evDetails,
	})
}

// controllerUnreachableGapMinDuration bounds the controller-unreachable gap
// bookkeeping to intervals that are meaningful for audit coverage; sub-second
// bootstrap delays are not recorded as gaps.
const controllerUnreachableGapMinDuration = 2 * time.Second

// presenceCheckpointInterval bounds how long an active connection may remain
// without any durable observation evidence. Connections that transfer no bytes
// still emit one lightweight ConnectionPresenceCheckpoint per interval so
// liveness, last-observed facts and relay overlap windows stay correct while
// zero-byte ConnectionDelta rows are suppressed.
const presenceCheckpointInterval = 30 * time.Second

// connectionPresencePrecision marks presence checkpoint events in storage.
const connectionPresencePrecision = "presence_checkpoint"

const controllerUnreachableGapReason = "controller_unreachable_since_session_start"

// emitUnobservedIntervalGap records [start, end] as a closed controller_stream
// monitoring gap pair. Intervals shorter than the minimum duration are skipped
// and the pair is emitted at most once per engine lifetime.
func (e *StateEngine) emitUnobservedIntervalGap(start, end time.Time) error {
	if e.unobservedGapEmitted || start.IsZero() || !end.After(start) {
		return nil
	}
	if end.Sub(start) < controllerUnreachableGapMinDuration {
		return nil
	}
	e.unobservedGapEmitted = true
	// The storage contract stores all gap interval bounds in UTC RFC3339; the
	// rest of the coverage pipeline compares them as UTC strings.
	startStr := start.UTC().Format(time.RFC3339Nano)
	endUTC := end.UTC().Format(time.RFC3339Nano)
	if err := e.emitEvent(&types.CollectorEvent{
		Type:                types.EventMonitoringGapOpened,
		Timestamp:           end,
		AttributionInterval: []string{startStr},
		Details: map[string]any{
			"reason": controllerUnreachableGapReason,
		},
	}); err != nil {
		return err
	}
	return e.emitEvent(&types.CollectorEvent{
		Type:                types.EventMonitoringGapClosed,
		Timestamp:           end,
		AttributionInterval: []string{startStr, endUTC},
		Details: map[string]any{
			"actualGapMs": end.Sub(start).Milliseconds(),
			"reason":      controllerUnreachableGapReason,
		},
	})
}

// EmitUnobservedSessionGap closes the session's observation accounting when the
// collector shuts down without ever processing a healthy controller frame: the
// session interval was spent unable to observe the controller and must be
// recorded as a controller_stream monitoring gap instead of counting as
// covered time. It is a no-op when the session ever observed a healthy frame.
func (e *StateEngine) EmitUnobservedSessionGap(shutdownAt time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.hasEverBeenHealthy {
		return nil
	}
	return e.emitUnobservedIntervalGap(e.createdAt, shutdownAt)
}

// emitDeltaOrPresence is the single shared durable-evidence decision contract
// for existing connections, applied identically by the steady-state path and
// the reconnect-recovery path. A positive traffic delta always emits a full
// ConnectionDelta built by buildDelta. A zero-byte frame emits no Delta; it
// emits one lightweight ConnectionPresenceCheckpoint only when the connection
// has had no durable evidence for presenceCheckpointInterval, so liveness and
// last-observed facts stay durably observable while raw evidence density is
// bounded. Delta and presence are never emitted at the same instant. The
// caller always updates in-memory state first; only durable density differs.
func (e *StateEngine) emitDeltaOrPresence(
	prev *ActiveConnectionState,
	c types.ConnectionSnapshot,
	frameTs time.Time,
	deltaUp, deltaDown int64,
	buildDelta func() *types.CollectorEvent,
) error {
	if deltaUp > 0 || deltaDown > 0 {
		if err := e.emitEvent(buildDelta()); err != nil {
			return err
		}
		prev.LastDurableEvidenceAt = frameTs
		return nil
	}
	if frameTs.Sub(prev.LastDurableEvidenceAt) < presenceCheckpointInterval {
		return nil
	}
	if err := e.emitEvent(&types.CollectorEvent{
		Type:                    types.EventConnectionPresenceCheckpoint,
		Timestamp:               frameTs,
		ConnectionID:            c.ID,
		ObservedUploadCounter:   c.Upload,
		ObservedDownloadCounter: c.Download,
		Precision:               connectionPresencePrecision,
	}); err != nil {
		return err
	}
	prev.LastDurableEvidenceAt = frameTs
	return nil
}

// ProcessIngestItem 统一处理有序通道中的项 (Frame, GapOpened, Health, Overload)
func (e *StateEngine) ProcessIngestItem(item *types.IngestItem) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	switch item.Kind {
	case types.ItemGapOpened:
		if e.hasEverBeenHealthy && !e.gapIsOpen {
			e.gapIsOpen = true
			gapStart := e.lastSuccessfullyProcessedHealthyFrameAt
			if gapStart.IsZero() {
				gapStart = item.Timestamp
			}
			e.gapStartTime = gapStart
			e.gapStartTimeMonotonic = e.lastHealthyMonotonic
			if e.gapStartTimeMonotonic.IsZero() {
				e.gapStartTimeMonotonic = time.Now()
			}
			e.gapStartTimeString = e.lastSuccessfullyProcessedHealthyStr
			if e.gapStartTimeString == "" {
				e.gapStartTimeString = gapStart.Format(time.RFC3339Nano)
			}
			e.gapInjectionDetails = item.Details
			e.sessionState = types.SessionReconnectBackoff

			details := map[string]any{
				"gapStart":             e.gapStartTimeString,
				"disconnectDetectedAt": item.Timestamp.Format(time.RFC3339Nano),
			}
			if item.Details != nil {
				for k, v := range item.Details {
					details[k] = v
				}
			}

			ev := &types.CollectorEvent{
				Type:                types.EventMonitoringGapOpened,
				Timestamp:           item.Timestamp,
				AttributionInterval: []string{e.gapStartTimeString},
				Details:             details,
			}
			return e.emitEvent(ev)
		}
		return nil

	case types.ItemCollectorHealth:
		ev := &types.CollectorEvent{
			Type:      types.EventCollectorHealth,
			Timestamp: item.Timestamp,
			Details: map[string]any{
				"issue":   item.HealthIssue,
				"details": item.Details,
			},
		}
		return e.emitEvent(ev)

	case types.ItemQueueOverload:
		ev := &types.CollectorEvent{
			Type:      types.EventCollectorHealth,
			Timestamp: item.Timestamp,
			Details: map[string]any{
				"issue":       "queue_overload_degradation",
				"description": "Collector input queue saturated, backpressure active",
			},
		}
		return e.emitEvent(ev)

	case types.ItemFrame:
		return e.processFrameInternal(item.Frame)

	default:
		return nil
	}
}

// ProcessFrame 兼容入口
func (e *StateEngine) ProcessFrame(frame *types.ConnectionSnapshotFrame) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.processFrameInternal(frame)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (e *StateEngine) processFrameInternal(frame *types.ConnectionSnapshotFrame) error {
	e.frameSequence++
	e.eventSequence = 0

	if memSink, ok := e.sink.(*sink.MemorySink); ok {
		memSink.IncrementFrames()
	} else if statsSink, ok := e.sink.(*sink.ProductionStatsSink); ok {
		statsSink.IncrementFrames()
	} else if valSink, ok := e.sink.(*sink.ValidationJSONLSink); ok {
		valSink.IncrementFrames()
	}

	payload := frame.GetPayload()
	frameTs := time.Now()
	if frame.ReceivedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, frame.ReceivedAt); err == nil {
			frameTs = t
		} else if t, err := time.Parse(time.RFC3339, frame.ReceivedAt); err == nil {
			frameTs = t
		}
	}
	frameTsStr := frame.ReceivedAt
	if frameTsStr == "" {
		frameTsStr = frameTs.Format(time.RFC3339Nano)
	}

	// -------------------------------------------------------------
	// 1. 重连恢复首帧处理 (Reconnect First Frame)
	// -------------------------------------------------------------
	if e.gapIsOpen {
		gapStartStr := e.gapStartTimeString
		if gapStartStr == "" {
			gapStartStr = e.lastSuccessfullyProcessedHealthyStr
		}

		monotonicGapDurationMs := time.Since(e.gapStartTimeMonotonic).Milliseconds()
		isEpochBreakAcrossGap := payload.UploadTotal < e.prevUploadTotal || payload.DownloadTotal < e.prevDownloadTotal

		if isEpochBreakAcrossGap {
			closedDetails := map[string]any{
				"actualGapMs":                 monotonicGapDurationMs,
				"gapPhysicalDeltaUnavailable": true,
				"reason":                      "epoch_reset_across_gap",
			}
			if e.gapInjectionDetails != nil {
				for k, v := range e.gapInjectionDetails {
					closedDetails[k] = v
				}
			}

			if err := e.emitEvent(&types.CollectorEvent{
				Type:                types.EventMonitoringGapClosed,
				Timestamp:           frameTs,
				AttributionInterval: []string{gapStartStr, frameTsStr},
				Details:             closedDetails,
			}); err != nil {
				return err
			}

			if err := e.emitEvent(&types.CollectorEvent{
				Type:      types.EventCounterEpochBreak,
				Timestamp: frameTs,
				Details: map[string]any{
					"prevUploadTotal":   e.prevUploadTotal,
					"currUploadTotal":   payload.UploadTotal,
					"prevDownloadTotal": e.prevDownloadTotal,
					"currDownloadTotal": payload.DownloadTotal,
					"acrossGap":         true,
				},
			}); err != nil {
				return err
			}

			e.epochID++
			e.activeMap = make(map[string]*ActiveConnectionState)
			e.resetTombstones()
			e.gapIsOpen = false
			e.isBootstrapFrame = true
		} else {
			e.sessionState = types.SessionRecovering
			globalGapUp := payload.UploadTotal - e.prevUploadTotal
			globalGapDown := payload.DownloadTotal - e.prevDownloadTotal

			closedDetails := map[string]any{
				"actualGapMs":            monotonicGapDurationMs,
				"globalGapUploadDelta":   globalGapUp,
				"globalGapDownloadDelta": globalGapDown,
			}
			if e.gapInjectionDetails != nil {
				for k, v := range e.gapInjectionDetails {
					closedDetails[k] = v
				}
			}

			if err := e.emitEvent(&types.CollectorEvent{
				Type:                types.EventMonitoringGapClosed,
				Timestamp:           frameTs,
				AttributionInterval: []string{gapStartStr, frameTsStr},
				Details:             closedDetails,
			}); err != nil {
				return err
			}

			currMap := make(map[string]types.ConnectionSnapshot, len(payload.Connections))
			for _, c := range payload.Connections {
				currMap[c.ID] = c
			}

			for _, c := range payload.Connections {
				id := c.ID
				if prev, exists := e.activeMap[id]; exists {
					deltaUp := c.Upload - prev.LastUploadCounter
					deltaDown := c.Download - prev.LastDownloadCounter

					if deltaUp < 0 || deltaDown < 0 {
						if err := e.emitEvent(&types.CollectorEvent{
							Type:         types.EventCollectorHealth,
							Timestamp:    frameTs,
							ConnectionID: id,
							Details: map[string]any{
								"issue": "connection_counter_regression_across_gap",
							},
						}); err != nil {
							return err
						}
						deltaUp = 0
						deltaDown = 0
						prev.LastDurableEvidenceAt = frameTs
					}

					prev.LastUploadCounter = c.Upload
					prev.LastDownloadCounter = c.Download
					prev.LastObservedAt = frameTs
					prev.MonitoredCumulativeUpload += deltaUp
					prev.MonitoredCumulativeDownload += deltaDown

					if err := e.emitDeltaOrPresence(prev, c, frameTs, deltaUp, deltaDown, func() *types.CollectorEvent {
						return &types.CollectorEvent{
							Type:                        types.EventConnectionDelta,
							Timestamp:                   frameTs,
							ConnectionID:                id,
							Metadata:                    c.Metadata,
							QualityFlags:                c.Metadata.DeriveQualityFlags(c.Rule, c.Chains),
							Rule:                        c.Rule,
							RulePayload:                 c.RulePayload,
							Chains:                      c.Chains,
							ProviderChains:              c.ProviderChains,
							Route:                       prev.Route,
							AttributionClass:            prev.AttributionClass,
							ObservedUploadCounter:       c.Upload,
							ObservedDownloadCounter:     c.Download,
							DeltaUpload:                 deltaUp,
							DeltaDownload:               deltaDown,
							MonitoredCumulativeUpload:   prev.MonitoredCumulativeUpload,
							MonitoredCumulativeDownload: prev.MonitoredCumulativeDownload,
							BaselineUploadCounter:       prev.BaselineUploadCounter,
							BaselineDownloadCounter:     prev.BaselineDownloadCounter,
							AttributionInterval:         []string{gapStartStr, frameTsStr},
							Precision:                   "interval_only",
						}
					}); err != nil {
						return err
					}
				} else {
					route := attribution.ClassifyRoute(c.Chains)
					attrClass := attribution.ClassifyInitialAttribution(&c)
					quality := c.Metadata.DeriveQualityFlags(c.Rule, c.Chains)

					startClass := "start_unknown"
					if c.Start != "" {
						if st, err := time.Parse(time.RFC3339Nano, c.Start); err == nil {
							if st.After(e.gapStartTime) || st.Equal(e.gapStartTime) {
								startClass = "started_inside_gap"
							} else {
								startClass = "started_before_gap_but_not_previously_visible"
							}
						} else if st, err := time.Parse(time.RFC3339, c.Start); err == nil {
							if st.After(e.gapStartTime) || st.Equal(e.gapStartTime) {
								startClass = "started_inside_gap"
							} else {
								startClass = "started_before_gap_but_not_previously_visible"
							}
						}
					}

					e.activeMap[id] = &ActiveConnectionState{
						Snapshot:                    c,
						FirstObservedAt:             frameTs,
						LastObservedAt:              frameTs,
						LastUploadCounter:           c.Upload,
						LastDownloadCounter:         c.Download,
						BaselineUploadCounter:       c.Upload,
						BaselineDownloadCounter:     c.Download,
						MonitoredCumulativeUpload:   0,
						MonitoredCumulativeDownload: 0,
						LastDurableEvidenceAt:       frameTs,
						Route:                       route,
						AttributionClass:            attrClass,
						QualityFlags:                quality,
					}

					if err := e.emitEvent(&types.CollectorEvent{
						Type:                        types.EventConnectionBootstrap,
						Timestamp:                   frameTs,
						ConnectionID:                id,
						Metadata:                    c.Metadata,
						QualityFlags:                quality,
						MihomoStart:                 c.Start,
						Rule:                        c.Rule,
						RulePayload:                 c.RulePayload,
						Chains:                      c.Chains,
						ProviderChains:              c.ProviderChains,
						Route:                       route,
						AttributionClass:            attrClass,
						ObservedUploadCounter:       c.Upload,
						ObservedDownloadCounter:     c.Download,
						DeltaUpload:                 0,
						DeltaDownload:               0,
						MonitoredCumulativeUpload:   0,
						MonitoredCumulativeDownload: 0,
						BaselineUploadCounter:       c.Upload,
						BaselineDownloadCounter:     c.Download,
						Precision:                   "gap_post_baseline",
						Details: map[string]any{
							"startClassification": startClass,
						},
					}); err != nil {
						return err
					}
				}
			}

			var disappearedIDs []string
			for id := range e.activeMap {
				if _, exists := currMap[id]; !exists {
					disappearedIDs = append(disappearedIDs, id)
				}
			}
			sort.Strings(disappearedIDs)

			for _, id := range disappearedIDs {
				prev := e.activeMap[id]
				if err := e.emitEvent(&types.CollectorEvent{
					Type:                   types.EventConnectionDisappeared,
					Timestamp:              frameTs,
					ConnectionID:           id,
					Metadata:               prev.Snapshot.Metadata,
					QualityFlags:           prev.QualityFlags,
					PossibleUnobservedTail: true,
				Details: map[string]any{
					"disappearedDuringGap":        true,
					"lastObservedAt":              prev.LastObservedAt.UTC().Format(time.RFC3339Nano),
					"lastObservedUploadCounter":   prev.LastUploadCounter,
					"lastObservedDownloadCounter": prev.LastDownloadCounter,
				},
			}); err != nil {
				return err
			}
			e.recordDisappearedTombstone(id, prev, frameTs)
			delete(e.activeMap, id)
		}

			e.prevUploadTotal = payload.UploadTotal
			e.prevDownloadTotal = payload.DownloadTotal
			e.lastSuccessfullyProcessedHealthyFrameAt = frameTs
			e.lastHealthyMonotonic = time.Now()
			e.lastSuccessfullyProcessedHealthyStr = frameTsStr
			e.gapIsOpen = false
			e.sessionState = types.SessionHealthy

			// Output SamplingResidual to complete the recovery snapshot frame.
			if err := e.emitEvent(&types.CollectorEvent{
				Type:      types.EventSamplingResidual,
				Timestamp: frameTs,
				Details: map[string]any{
					"globalUploadDelta":      globalGapUp,
					"globalDownloadDelta":    globalGapDown,
					"uniqueObservedUpload":   int64(0),
					"uniqueObservedDownload": int64(0),
					"residualUpload":         int64(0),
					"residualDownload":       int64(0),
					"frameType":              "recovery",
				},
			}); err != nil {
				return err
			}
			return nil
		}
	}

	// -------------------------------------------------------------
	// 2. 稳态中的 Epoch Break 检测
	// -------------------------------------------------------------
	if !e.isBootstrapFrame {
		isEpochBreak := payload.UploadTotal < e.prevUploadTotal || payload.DownloadTotal < e.prevDownloadTotal
		if isEpochBreak {
			if err := e.emitEvent(&types.CollectorEvent{
				Type:      types.EventCounterEpochBreak,
				Timestamp: frameTs,
				Details: map[string]any{
					"prevUploadTotal":   e.prevUploadTotal,
					"currUploadTotal":   payload.UploadTotal,
					"prevDownloadTotal": e.prevDownloadTotal,
					"currDownloadTotal": payload.DownloadTotal,
				},
			}); err != nil {
				return err
			}
			e.epochID++
			e.activeMap = make(map[string]*ActiveConnectionState)
			e.resetTombstones()
			e.isBootstrapFrame = true
		}
	}

	// -------------------------------------------------------------
	// 3. Bootstrap 首帧处理 (冷启动或 Epoch Break 后)
	// -------------------------------------------------------------
	if e.isBootstrapFrame {
		e.sessionState = types.SessionBootstrap
		e.activeMap = make(map[string]*ActiveConnectionState, len(payload.Connections))
		e.resetTombstones()

		for _, c := range payload.Connections {
			route := attribution.ClassifyRoute(c.Chains)
			attrClass := attribution.ClassifyInitialAttribution(&c)
			quality := c.Metadata.DeriveQualityFlags(c.Rule, c.Chains)

			state := &ActiveConnectionState{
				Snapshot:                    c,
				FirstObservedAt:             frameTs,
				LastObservedAt:              frameTs,
				LastUploadCounter:           c.Upload,
				LastDownloadCounter:         c.Download,
				BaselineUploadCounter:       c.Upload,
				BaselineDownloadCounter:     c.Download,
				MonitoredCumulativeUpload:   0,
				MonitoredCumulativeDownload: 0,
				LastDurableEvidenceAt:       frameTs,
				PreexistingAtStart:          true,
				Route:                       route,
				AttributionClass:            attrClass,
				QualityFlags:                quality,
			}
			e.activeMap[c.ID] = state

			if err := e.emitEvent(&types.CollectorEvent{
				Type:                        types.EventConnectionBootstrap,
				Timestamp:                   frameTs,
				ConnectionID:                c.ID,
				Metadata:                    c.Metadata,
				QualityFlags:                quality,
				MihomoStart:                 c.Start,
				Rule:                        c.Rule,
				RulePayload:                 c.RulePayload,
				Chains:                      c.Chains,
				ProviderChains:              c.ProviderChains,
				Route:                       route,
				AttributionClass:            attrClass,
				ObservedUploadCounter:       c.Upload,
				ObservedDownloadCounter:     c.Download,
				DeltaUpload:                 0,
				DeltaDownload:               0,
				MonitoredCumulativeUpload:   0,
				MonitoredCumulativeDownload: 0,
				BaselineUploadCounter:       c.Upload,
				BaselineDownloadCounter:     c.Download,
				PreexistingAtStart:          true,
			}); err != nil {
				return err
			}
		}

		e.prevUploadTotal = payload.UploadTotal
		e.prevDownloadTotal = payload.DownloadTotal
		e.lastSuccessfullyProcessedHealthyFrameAt = frameTs
		e.lastHealthyMonotonic = time.Now()
		e.lastSuccessfullyProcessedHealthyStr = frameTsStr
		if !e.hasEverBeenHealthy {
			// The controller was unreachable from session start until this
			// first healthy frame. That whole interval is unobserved time and
			// must be recorded as a controller_stream monitoring gap instead of
			// silently counting as covered.
			if err := e.emitUnobservedIntervalGap(e.createdAt, frameTs); err != nil {
				return err
			}
		}
		e.hasEverBeenHealthy = true
		e.isBootstrapFrame = false
		e.sessionState = types.SessionHealthy

		// Output SamplingResidual to complete the bootstrap snapshot frame.
		if err := e.emitEvent(&types.CollectorEvent{
			Type:      types.EventSamplingResidual,
			Timestamp: frameTs,
			Details: map[string]any{
				"globalUploadDelta":      int64(0),
				"globalDownloadDelta":    int64(0),
				"uniqueObservedUpload":   int64(0),
				"uniqueObservedDownload": int64(0),
				"residualUpload":         int64(0),
				"residualDownload":       int64(0),
				"frameType":              "bootstrap",
			},
		}); err != nil {
			return err
		}
		return nil
	}

	// -------------------------------------------------------------
	// 4. 稳态快照帧处理 (Steady-State Processing)
	// -------------------------------------------------------------
	currMap := make(map[string]types.ConnectionSnapshot, len(payload.Connections))
	deltas := make(map[string][2]int64, len(payload.Connections))
	// reobservations holds same-ID + same-Start reappearance continuations:
	// their delta is the counter difference since the last durable observation,
	// never the full lifetime counters (which would double count the bytes
	// already monitored before the disappearance).
	reobservations := make(map[string]*reobservationContinuation)

	for _, c := range payload.Connections {
		currMap[c.ID] = c
		if prev, exists := e.activeMap[c.ID]; exists {
			dUp := c.Upload - prev.LastUploadCounter
			dDown := c.Download - prev.LastDownloadCounter
			if dUp < 0 {
				dUp = 0
			}
			if dDown < 0 {
				dDown = 0
			}
			deltas[c.ID] = [2]int64{dUp, dDown}
		} else if tomb, isReobservation := e.disappearedTombstones[c.ID]; isReobservation && c.Start == tomb.state.Snapshot.Start {
			// Same Mihomo connection (same ID + same Mihomo start) reappearing
			// after a snapshot flap: continue the previous lifecycle. A counter
			// regression against the tombstone is anomalous evidence and is
			// clamped to zero with a health event, mirroring the connected-path
			// regression contract.
			cont := &reobservationContinuation{tomb: tomb}
			dUp := c.Upload - tomb.state.LastUploadCounter
			dDown := c.Download - tomb.state.LastDownloadCounter
			if dUp < 0 || dDown < 0 {
				cont.regressed = true
				if dUp < 0 {
					dUp = 0
				}
				if dDown < 0 {
					dDown = 0
				}
			}
			cont.deltaUp = dUp
			cont.deltaDown = dDown
			deltas[c.ID] = [2]int64{dUp, dDown}
			reobservations[c.ID] = cont
		} else {
			deltas[c.ID] = [2]int64{c.Upload, c.Download}
		}
	}

	confirmedRelays := attribution.PerformFrameRelayDeduplication(payload.Connections, deltas)

	var uniqueObservedUpload int64
	var uniqueObservedDownload int64

	for _, c := range payload.Connections {
		id := c.ID
		delta := deltas[id]
		deltaUp := delta[0]
		deltaDown := delta[1]
		quality := c.Metadata.DeriveQualityFlags(c.Rule, c.Chains)

		if prev, exists := e.activeMap[id]; exists {
			// 1. 全字段元数据演化与路由突变检测
			chainsChanged := !slicesEqual(prev.Snapshot.Chains, c.Chains)
			metaChanged := prev.Snapshot.Metadata != c.Metadata ||
				prev.Snapshot.Rule != c.Rule ||
				prev.Snapshot.RulePayload != c.RulePayload ||
				!slicesEqual(prev.Snapshot.ProviderChains, c.ProviderChains) ||
				chainsChanged ||
				prev.QualityFlags != quality

			if chainsChanged {
				if err := e.emitEvent(&types.CollectorEvent{
					Type:         types.EventCollectorHealth,
					Timestamp:    frameTs,
					ConnectionID: id,
					Details: map[string]any{
						"issue":      "routing_chain_mutation_observed",
						"prevChains": prev.Snapshot.Chains,
						"currChains": c.Chains,
					},
				}); err != nil {
					return err
				}
				prev.Route = attribution.ClassifyRoute(c.Chains)
				prev.LastDurableEvidenceAt = frameTs
			}

			if metaChanged {
				prev.Snapshot = c
				prev.QualityFlags = quality
				if err := e.emitEvent(&types.CollectorEvent{
					Type:           types.EventConnectionMetadataUpdated,
					Timestamp:      frameTs,
					ConnectionID:   id,
					Metadata:       c.Metadata,
					QualityFlags:   quality,
					Rule:           c.Rule,
					RulePayload:    c.RulePayload,
					Chains:         c.Chains,
					ProviderChains: c.ProviderChains,
					Route:          prev.Route,
				}); err != nil {
					return err
				}
				prev.LastDurableEvidenceAt = frameTs
			}

			// 2. Per-connection 计数器回退防护
			if c.Upload < prev.LastUploadCounter || c.Download < prev.LastDownloadCounter {
				if err := e.emitEvent(&types.CollectorEvent{
					Type:         types.EventCollectorHealth,
					Timestamp:    frameTs,
					ConnectionID: id,
					Details: map[string]any{
						"issue":        "connection_counter_regression",
						"prevUpload":   prev.LastUploadCounter,
						"currUpload":   c.Upload,
						"prevDownload": prev.LastDownloadCounter,
						"currDownload": c.Download,
					},
				}); err != nil {
					return err
				}
				prev.LastUploadCounter = c.Upload
				prev.LastDownloadCounter = c.Download
				prev.LastDurableEvidenceAt = frameTs
				continue
			}

			// 3. 归因类别变更检测 (Relay Duplicate Confirmed)
			targetClass := prev.AttributionClass
			var relayEvidence map[string]any
			if ev, isConfirmed := confirmedRelays[id]; isConfirmed {
				targetClass = types.ClassConfirmedRelayDuplicate
				relayEvidence = ev
			}

			if targetClass != prev.AttributionClass {
				prev.AttributionClass = targetClass
				if err := e.emitEvent(&types.CollectorEvent{
					Type:             types.EventRelayClassificationChanged,
					Timestamp:        frameTs,
					ConnectionID:     id,
					AttributionClass: targetClass,
					Details:          relayEvidence,
				}); err != nil {
					return err
				}
				prev.LastDurableEvidenceAt = frameTs
			}

			prev.LastUploadCounter = c.Upload
			prev.LastDownloadCounter = c.Download
			prev.LastObservedAt = frameTs
			prev.MonitoredCumulativeUpload += deltaUp
			prev.MonitoredCumulativeDownload += deltaDown

			if prev.AttributionClass != types.ClassConfirmedRelayDuplicate {
				uniqueObservedUpload += deltaUp
				uniqueObservedDownload += deltaDown
			}

			if err := e.emitDeltaOrPresence(prev, c, frameTs, deltaUp, deltaDown, func() *types.CollectorEvent {
				return &types.CollectorEvent{
					Type:                        types.EventConnectionDelta,
					Timestamp:                   frameTs,
					ConnectionID:                id,
					Metadata:                    c.Metadata,
					QualityFlags:                quality,
					Rule:                        c.Rule,
					RulePayload:                 c.RulePayload,
					Chains:                      c.Chains,
					ProviderChains:              c.ProviderChains,
					Route:                       prev.Route,
					AttributionClass:            prev.AttributionClass,
					ObservedUploadCounter:       c.Upload,
					ObservedDownloadCounter:     c.Download,
					DeltaUpload:                 deltaUp,
					DeltaDownload:               deltaDown,
					MonitoredCumulativeUpload:   prev.MonitoredCumulativeUpload,
					MonitoredCumulativeDownload: prev.MonitoredCumulativeDownload,
					BaselineUploadCounter:       prev.BaselineUploadCounter,
					BaselineDownloadCounter:     prev.BaselineDownloadCounter,
					Details:                     relayEvidence,
				}
			}); err != nil {
				return err
			}
		} else if cont, isReobservation := reobservations[id]; isReobservation {
			// Same-ID + same-Start reappearance: reopen the previous lifecycle
			// instead of counting it as a new connection. Monitored totals and
			// the baseline carry over from the tombstone; only the counter
			// difference is new observed traffic. The journal keeps both the
			// Disappeared and this re-observation New event verbatim.
			tomb := cont.tomb
			prevTombState := tomb.state
			route := attribution.ClassifyRoute(c.Chains)
			initialClass := attribution.ClassifyInitialAttribution(&c)
			var relayEvidence map[string]any
			if ev, isConfirmed := confirmedRelays[id]; isConfirmed {
				initialClass = types.ClassConfirmedRelayDuplicate
				relayEvidence = ev
			}

			if cont.regressed {
				if err := e.emitEvent(&types.CollectorEvent{
					Type:         types.EventCollectorHealth,
					Timestamp:    frameTs,
					ConnectionID: id,
					Details: map[string]any{
						"issue":            "connection_counter_regression_on_reobservation",
						"prevUpload":       prevTombState.LastUploadCounter,
						"currUpload":       c.Upload,
						"prevDownload":     prevTombState.LastDownloadCounter,
						"currDownload":     c.Download,
						"sameMihomoStart":  true,
						"disappearedAt":    tomb.disappearedAt.UTC().Format(time.RFC3339Nano),
						"lastObservedAt":   prevTombState.LastObservedAt.UTC().Format(time.RFC3339Nano),
					},
				}); err != nil {
					return err
				}
			}

			state := &ActiveConnectionState{
				Snapshot:                    c,
				FirstObservedAt:             prevTombState.FirstObservedAt,
				LastObservedAt:              frameTs,
				LastUploadCounter:           c.Upload,
				LastDownloadCounter:         c.Download,
				MonitoredCumulativeUpload:   prevTombState.MonitoredCumulativeUpload + cont.deltaUp,
				MonitoredCumulativeDownload: prevTombState.MonitoredCumulativeDownload + cont.deltaDown,
				BaselineUploadCounter:       prevTombState.BaselineUploadCounter,
				BaselineDownloadCounter:     prevTombState.BaselineDownloadCounter,
				LastDurableEvidenceAt:       frameTs,
				PreexistingAtStart:          prevTombState.PreexistingAtStart,
				Route:                       route,
				AttributionClass:            initialClass,
				QualityFlags:                quality,
			}
			e.activeMap[id] = state
			e.takeDisappearedTombstone(id)

			if initialClass != types.ClassConfirmedRelayDuplicate {
				uniqueObservedUpload += cont.deltaUp
				uniqueObservedDownload += cont.deltaDown
			}

			reobsEv := &types.CollectorEvent{
				Type:                        types.EventConnectionNew,
				Timestamp:                   frameTs,
				ConnectionID:                id,
				Metadata:                    c.Metadata,
				QualityFlags:                quality,
				MihomoStart:                 c.Start,
				Rule:                        c.Rule,
				RulePayload:                 c.RulePayload,
				Chains:                      c.Chains,
				ProviderChains:              c.ProviderChains,
				Route:                       route,
				AttributionClass:            initialClass,
				ObservedUploadCounter:       c.Upload,
				ObservedDownloadCounter:     c.Download,
				DeltaUpload:                 cont.deltaUp,
				DeltaDownload:               cont.deltaDown,
				MonitoredCumulativeUpload:   state.MonitoredCumulativeUpload,
				MonitoredCumulativeDownload: state.MonitoredCumulativeDownload,
				BaselineUploadCounter:       state.BaselineUploadCounter,
				BaselineDownloadCounter:     state.BaselineDownloadCounter,
				Details:                     reobservationDetails(tomb, prevTombState, relayEvidence),
			}
			// The continuation delta covers the whole flap window rather than a
			// single sample interval, so it is interval-attributed evidence from
			// the last durable observation to the reappearance frame.
			if cont.deltaUp > 0 || cont.deltaDown > 0 {
				reobsEv.Precision = "interval_only"
				reobsEv.AttributionInterval = []string{
					prevTombState.LastObservedAt.UTC().Format(time.RFC3339Nano),
					frameTs.UTC().Format(time.RFC3339Nano),
				}
			}
			if err := e.emitEvent(reobsEv); err != nil {
				return err
			}
		} else {
			// Any prior tombstone for this ID is now replaced/consumed by the new
			// connection (e.g. different Start or first appearance).
			e.takeDisappearedTombstone(id)
			route := attribution.ClassifyRoute(c.Chains)
			initialClass := attribution.ClassifyInitialAttribution(&c)
			var relayEvidence map[string]any
			if ev, isConfirmed := confirmedRelays[id]; isConfirmed {
				initialClass = types.ClassConfirmedRelayDuplicate
				relayEvidence = ev
			}

			state := &ActiveConnectionState{
				Snapshot:                    c,
				FirstObservedAt:             frameTs,
				LastObservedAt:              frameTs,
				LastUploadCounter:           c.Upload,
				LastDownloadCounter:         c.Download,
				BaselineUploadCounter:       0,
				BaselineDownloadCounter:     0,
				MonitoredCumulativeUpload:   deltaUp,
				MonitoredCumulativeDownload: deltaDown,
				LastDurableEvidenceAt:       frameTs,
				Route:                       route,
				AttributionClass:            initialClass,
				QualityFlags:                quality,
			}
			e.activeMap[id] = state

			if initialClass != types.ClassConfirmedRelayDuplicate {
				uniqueObservedUpload += deltaUp
				uniqueObservedDownload += deltaDown
			}

			if err := e.emitEvent(&types.CollectorEvent{
				Type:                        types.EventConnectionNew,
				Timestamp:                   frameTs,
				ConnectionID:                id,
				Metadata:                    c.Metadata,
				QualityFlags:                quality,
				MihomoStart:                 c.Start,
				Rule:                        c.Rule,
				RulePayload:                 c.RulePayload,
				Chains:                      c.Chains,
				ProviderChains:              c.ProviderChains,
				Route:                       route,
				AttributionClass:            initialClass,
				ObservedUploadCounter:       c.Upload,
				ObservedDownloadCounter:     c.Download,
				DeltaUpload:                 deltaUp,
				DeltaDownload:               deltaDown,
				MonitoredCumulativeUpload:   deltaUp,
				MonitoredCumulativeDownload: deltaDown,
				BaselineUploadCounter:       0,
				BaselineDownloadCounter:     0,
				Details:                     relayEvidence,
			}); err != nil {
				return err
			}
		}
	}

	// 消失连接检测 (通过排序保证确定性)
	var disappearedIDs []string
	for id := range e.activeMap {
		if _, exists := currMap[id]; !exists {
			disappearedIDs = append(disappearedIDs, id)
		}
	}
	sort.Strings(disappearedIDs)

	for _, id := range disappearedIDs {
		prev := e.activeMap[id]
		if err := e.emitEvent(&types.CollectorEvent{
			Type:                   types.EventConnectionDisappeared,
			Timestamp:              frameTs,
			ConnectionID:           id,
			Metadata:               prev.Snapshot.Metadata,
			QualityFlags:           prev.QualityFlags,
			PossibleUnobservedTail: true,
			Details: map[string]any{
				"lastObservedAt":              prev.LastObservedAt.UTC().Format(time.RFC3339Nano),
				"lastObservedUploadCounter":   prev.LastUploadCounter,
				"lastObservedDownloadCounter": prev.LastDownloadCounter,
			},
		}); err != nil {
			return err
		}
		e.recordDisappearedTombstone(id, prev, frameTs)
		delete(e.activeMap, id)
	}

	// 全局残差计算 (Residual = GlobalDelta - UniqueObserved)
	globalUpDelta := payload.UploadTotal - e.prevUploadTotal
	globalDownDelta := payload.DownloadTotal - e.prevDownloadTotal

	residualUp := globalUpDelta - uniqueObservedUpload
	residualDown := globalDownDelta - uniqueObservedDownload

	if err := e.emitEvent(&types.CollectorEvent{
		Type:      types.EventSamplingResidual,
		Timestamp: frameTs,
		Details: map[string]any{
			"globalUploadDelta":      globalUpDelta,
			"globalDownloadDelta":    globalDownDelta,
			"uniqueObservedUpload":   uniqueObservedUpload,
			"uniqueObservedDownload": uniqueObservedDownload,
			"residualUpload":         residualUp,
			"residualDownload":       residualDown,
		},
	}); err != nil {
		return err
	}

	e.prevUploadTotal = payload.UploadTotal
	e.prevDownloadTotal = payload.DownloadTotal
	e.lastSuccessfullyProcessedHealthyFrameAt = frameTs
	e.lastHealthyMonotonic = time.Now()
	e.lastSuccessfullyProcessedHealthyStr = frameTsStr
	return nil
}

// GetActiveConnectionsCount 获取当前活跃连接数
func (e *StateEngine) GetActiveConnectionsCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.activeMap)
}

// GetSessionState 获取当前会话状态
func (e *StateEngine) GetSessionState() types.SessionState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionState
}
