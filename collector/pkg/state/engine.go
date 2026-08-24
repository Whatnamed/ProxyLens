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
	Snapshot                  types.ConnectionSnapshot
	FirstObservedAt           time.Time
	LastObservedAt            time.Time
	LastUploadCounter         int64
	LastDownloadCounter       int64
	MonitoredCumulativeUpload int64
	MonitoredCumulativeDownload int64
	BaselineUploadCounter     int64
	BaselineDownloadCounter   int64
	PreexistingAtStart        bool
	PossibleUnobservedTail    bool
	Route                     types.RouteType
	AttributionClass          types.AttributionClass
	QualityFlags              types.QualityFlags
}

// EngineOptions 配置状态机参数
type EngineOptions struct {
	Sink      sink.EventSink
	SessionID string
}

// StateEngine 是 Collector 的核心状态机实现
type StateEngine struct {
	mu                                     sync.Mutex
	sink                                   sink.EventSink
	sessionID                              string
	epochID                                int
	frameSequence                          int64
	eventSequence                          int64
	sessionState                           types.SessionState
	activeMap                              map[string]*ActiveConnectionState
	prevUploadTotal                        int64
	prevDownloadTotal                      int64
	hasEverBeenHealthy                     bool
	gapIsOpen                              bool
	gapStartTime                           time.Time
	gapStartTimeString                     string
	lastSuccessfullyProcessedHealthyFrameAt time.Time
	lastSuccessfullyProcessedHealthyStr    string
	isBootstrapFrame                       bool
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
		sink:             s,
		sessionID:        sessID,
		epochID:          1,
		sessionState:     types.SessionStarting,
		activeMap:        make(map[string]*ActiveConnectionState),
		isBootstrapFrame: true,
	}
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
			e.gapStartTimeString = e.lastSuccessfullyProcessedHealthyStr
			if e.gapStartTimeString == "" {
				e.gapStartTimeString = gapStart.Format(time.RFC3339Nano)
			}
			e.sessionState = types.SessionReconnectBackoff

			ev := &types.CollectorEvent{
				Type:                types.EventMonitoringGapOpened,
				Timestamp:           item.Timestamp,
				AttributionInterval: []string{e.gapStartTimeString},
				Details: map[string]any{
					"gapStart":             e.gapStartTimeString,
					"disconnectDetectedAt": item.Timestamp.Format(time.RFC3339Nano),
				},
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
				"description": "Collector input queue saturated, possible unobserved frames",
			},
		}
		return e.emitEvent(ev)

	case types.ItemFrame:
		return e.processFrameInternal(item.Frame)

	default:
		return nil
	}
}

// ProcessFrame 兼容旧直接调用入口（内部转调 processFrameInternal）
func (e *StateEngine) ProcessFrame(frame *types.ConnectionSnapshotFrame) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.processFrameInternal(frame)
}

// processFrameInternal 串行执行确定性状态机计算
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

		// 检查 Gap 期间是否发生 Global Epoch Reset
		isEpochBreakAcrossGap := payload.UploadTotal < e.prevUploadTotal || payload.DownloadTotal < e.prevDownloadTotal

		if isEpochBreakAcrossGap {
			// 关闭 Gap 但标记区间物理流量不可用
			if err := e.emitEvent(&types.CollectorEvent{
				Type:                types.EventMonitoringGapClosed,
				Timestamp:           frameTs,
				AttributionInterval: []string{gapStartStr, frameTsStr},
				Details: map[string]any{
					"actualGapMs":                 frameTs.Sub(e.gapStartTime).Milliseconds(),
					"gapPhysicalDeltaUnavailable": true,
					"reason":                      "epoch_reset_across_gap",
				},
			}); err != nil {
				return err
			}

			// 触发 Epoch Break
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
			e.gapIsOpen = false
			e.isBootstrapFrame = true
			// 继续进入下方的 Bootstrap 处理
		} else {
			// 正常 Gap 恢复（Epoch 连续）
			e.sessionState = types.SessionRecovering
			globalGapUp := payload.UploadTotal - e.prevUploadTotal
			globalGapDown := payload.DownloadTotal - e.prevDownloadTotal

			if err := e.emitEvent(&types.CollectorEvent{
				Type:                types.EventMonitoringGapClosed,
				Timestamp:           frameTs,
				AttributionInterval: []string{gapStartStr, frameTsStr},
				Details: map[string]any{
					"actualGapMs":           frameTs.Sub(e.gapStartTime).Milliseconds(),
					"globalGapUploadDelta":   globalGapUp,
					"globalGapDownloadDelta": globalGapDown,
				},
			}); err != nil {
				return err
			}

			currMap := make(map[string]types.ConnectionSnapshot, len(payload.Connections))
			for _, c := range payload.Connections {
				currMap[c.ID] = c
			}

			// a. 跨 Gap 存活连接与新增连接（严格按切片原始顺序）
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
					}

					prev.LastUploadCounter = c.Upload
					prev.LastDownloadCounter = c.Download
					prev.LastObservedAt = frameTs
					prev.MonitoredCumulativeUpload += deltaUp
					prev.MonitoredCumulativeDownload += deltaDown

					if err := e.emitEvent(&types.CollectorEvent{
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
					}); err != nil {
						return err
					}
				} else {
					// b. 恢复后首次出现的连接 (确定性分类与 Baseline 归属)
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

			// c. 在 Gap 期间消失的连接 (ID 排序保证确定性)
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
						"disappearedDuringGap": true,
					},
				}); err != nil {
					return err
				}
				delete(e.activeMap, id)
			}

			e.prevUploadTotal = payload.UploadTotal
			e.prevDownloadTotal = payload.DownloadTotal
			e.lastSuccessfullyProcessedHealthyFrameAt = frameTs
			e.lastSuccessfullyProcessedHealthyStr = frameTsStr
			e.gapIsOpen = false
			e.sessionState = types.SessionHealthy
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
			e.isBootstrapFrame = true
		}
	}

	// -------------------------------------------------------------
	// 3. Bootstrap 首帧处理 (冷启动或 Epoch Break 后)
	// -------------------------------------------------------------
	if e.isBootstrapFrame {
		e.sessionState = types.SessionBootstrap
		e.activeMap = make(map[string]*ActiveConnectionState, len(payload.Connections))

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
				PreexistingAtStart:          true,
				Route:                       route,
				AttributionClass:            attrClass,
				QualityFlags:                quality,
			}
			e.activeMap[c.ID] = state

			// Bootstrap 帧严格输出 Delta = 0, Monitored = 0
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
		e.lastSuccessfullyProcessedHealthyStr = frameTsStr
		e.hasEverBeenHealthy = true
		e.isBootstrapFrame = false
		e.sessionState = types.SessionHealthy
		return nil
	}

	// -------------------------------------------------------------
	// 4. 稳态快照帧处理 (Steady-State Processing)
	// -------------------------------------------------------------
	currMap := make(map[string]types.ConnectionSnapshot, len(payload.Connections))
	deltas := make(map[string][2]int64, len(payload.Connections))

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
		} else {
			deltas[c.ID] = [2]int64{c.Upload, c.Download}
		}
	}

	// 执行生产级 Relay 结构去重匹配 (无歧义 1-to-1)
	confirmedRelays := attribution.PerformFrameRelayDeduplication(payload.Connections, deltas)

	var uniqueObservedUpload int64
	var uniqueObservedDownload int64

	// a. 更新存活连接与新增连接 (严格按 Connections 原始顺序处理)
	for _, c := range payload.Connections {
		id := c.ID
		delta := deltas[id]
		deltaUp := delta[0]
		deltaDown := delta[1]
		quality := c.Metadata.DeriveQualityFlags(c.Rule, c.Chains)

		if prev, exists := e.activeMap[id]; exists {
			// 1. 元数据演化检测 (Metadata Evolution)
			metaChanged := prev.Snapshot.Metadata.Process != c.Metadata.Process ||
				prev.Snapshot.Metadata.Host != c.Metadata.Host ||
				prev.Snapshot.Metadata.SniffHost != c.Metadata.SniffHost ||
				prev.Snapshot.Rule != c.Rule ||
				prev.Snapshot.RulePayload != c.RulePayload ||
				prev.QualityFlags != quality

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
				}); err != nil {
					return err
				}
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

			if err := e.emitEvent(&types.CollectorEvent{
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
			}); err != nil {
				return err
			}
		} else {
			// 稳态首次出现的新连接
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

	// b. 消失连接检测 (通过排序保证确定性)
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
		}); err != nil {
			return err
		}
		delete(e.activeMap, id)
	}

	// c. 全局残差计算 (Residual = GlobalDelta - UniqueObserved)
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
	e.lastSuccessfullyProcessedHealthyStr = frameTsStr
	return nil
}

// MarkGapOpened 外部显式通知断线
func (e *StateEngine) MarkGapOpened(t time.Time) {
	_ = e.ProcessIngestItem(&types.IngestItem{
		Kind:      types.ItemGapOpened,
		Timestamp: t,
	})
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
