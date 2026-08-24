package state

import (
	"sync"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/attribution"
	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// ActiveConnectionState 维护单个存活连接的内部状态
type ActiveConnectionState struct {
	Snapshot               types.ConnectionSnapshot
	FirstObservedAt        time.Time
	LastObservedAt         time.Time
	LastUpload             int64
	LastDownload           int64
	CumulativeUpload       int64
	CumulativeDownload     int64
	PreexistingAtStart     bool
	PossibleUnobservedTail bool
	Route                  types.RouteType
	AttributionClass       types.AttributionClass
}

// EngineOptions 配置状态机参数
type EngineOptions struct {
	Sink sink.EventSink
}

// StateEngine 是 Collector 的核心状态机实现
type StateEngine struct {
	mu                         sync.Mutex
	sink                       sink.EventSink
	sessionState               types.SessionState
	activeMap                  map[string]*ActiveConnectionState
	prevUploadTotal            int64
	prevDownloadTotal          int64
	lastHealthyTimestamp       time.Time
	lastHealthyTimeString      string
	isBootstrapFrame           bool
	isFirstFrameAfterReconnect bool
	gapStartTime               time.Time
	gapStartTimeString         string
}

// NewStateEngine 创建 StateEngine 实例
func NewStateEngine(opts EngineOptions) *StateEngine {
	s := opts.Sink
	if s == nil {
		s = sink.NewMemorySink()
	}
	return &StateEngine{
		sink:             s,
		sessionState:     types.SessionStarting,
		activeMap:        make(map[string]*ActiveConnectionState),
		isBootstrapFrame: true,
	}
}

// MarkGapOpened 记录监控断线开始
func (e *StateEngine) MarkGapOpened(t time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.sessionState = types.SessionReconnectBackoff
	e.gapStartTime = t
	e.gapStartTimeString = t.Format(time.RFC3339Nano)
	e.isFirstFrameAfterReconnect = true
}

// ProcessFrame 处理单个快照帧
func (e *StateEngine) ProcessFrame(frame *types.ConnectionSnapshotFrame) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if memSink, ok := e.sink.(*sink.MemorySink); ok {
		memSink.IncrementFrames()
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

	// 1. Counter Reset / Epoch Break 检测
	isEpochBreak := !e.isBootstrapFrame && !e.isFirstFrameAfterReconnect &&
		(payload.UploadTotal < e.prevUploadTotal || payload.DownloadTotal < e.prevDownloadTotal)

	if isEpochBreak {
		e.sink.Emit(&types.CollectorEvent{
			Type:      types.EventCounterEpochBreak,
			Timestamp: frameTs,
			Details: map[string]any{
				"prevUploadTotal":   e.prevUploadTotal,
				"currUploadTotal":   payload.UploadTotal,
				"prevDownloadTotal": e.prevDownloadTotal,
				"currDownloadTotal": payload.DownloadTotal,
			},
		})
		e.activeMap = make(map[string]*ActiveConnectionState)
		e.isBootstrapFrame = true
	}

	// 2. Bootstrap 首帧处理 (冷启动或 Epoch Break 重启后)
	if e.isBootstrapFrame {
		e.sessionState = types.SessionBootstrap
		e.activeMap = make(map[string]*ActiveConnectionState, len(payload.Connections))

		for _, c := range payload.Connections {
			route := attribution.ClassifyRoute(c.Chains)
			attrClass := attribution.ClassifyInitialAttribution(&c)

			state := &ActiveConnectionState{
				Snapshot:           c,
				FirstObservedAt:    frameTs,
				LastObservedAt:     frameTs,
				LastUpload:         c.Upload,
				LastDownload:       c.Download,
				PreexistingAtStart: true,
				Route:              route,
				AttributionClass:   attrClass,
			}
			e.activeMap[c.ID] = state

			// 严格遵守规约：Bootstrap 帧产生的 Delta = 0
			e.sink.Emit(&types.CollectorEvent{
				Type:               types.EventConnectionBootstrap,
				Timestamp:          frameTs,
				ConnectionID:       c.ID,
				Process:            c.Metadata.Process,
				ProcessPath:        c.Metadata.ProcessPath,
				Host:               c.Metadata.Host,
				DestinationIP:      c.Metadata.DestinationIP,
				DestinationPort:    c.Metadata.DestinationPort,
				Rule:               c.Rule,
				RulePayload:        c.RulePayload,
				Chains:             c.Chains,
				Route:              route,
				AttributionClass:   attrClass,
				DeltaUpload:        0,
				DeltaDownload:      0,
				CumulativeUpload:   c.Upload,
				CumulativeDownload: c.Download,
				PreexistingAtStart: true,
			})
		}

		e.prevUploadTotal = payload.UploadTotal
		e.prevDownloadTotal = payload.DownloadTotal
		e.lastHealthyTimestamp = frameTs
		e.lastHealthyTimeString = frameTsStr
		e.isBootstrapFrame = false
		e.sessionState = types.SessionHealthy
		return nil
	}

	// 3. Gap 恢复后首帧处理 (Reconnect First Frame)
	if e.isFirstFrameAfterReconnect {
		e.sessionState = types.SessionRecovering

		gapStartStr := e.gapStartTimeString
		if gapStartStr == "" {
			gapStartStr = e.lastHealthyTimeString
		}

		// 广播 Gap 结束事件
		e.sink.Emit(&types.CollectorEvent{
			Type:      types.EventMonitoringGapClosed,
			Timestamp: frameTs,
			AttributionInterval: []string{
				gapStartStr,
				frameTsStr,
			},
			Details: map[string]any{
				"actualGapMs": frameTs.Sub(e.lastHealthyTimestamp).Milliseconds(),
			},
		})

		currMap := make(map[string]types.ConnectionSnapshot, len(payload.Connections))
		for _, c := range payload.Connections {
			currMap[c.ID] = c
		}

		// 分类 1: 跨 Gap 存活连接
		for id, c := range currMap {
			if prev, exists := e.activeMap[id]; exists {
				deltaUp := c.Upload - prev.LastUpload
				deltaDown := c.Download - prev.LastDownload
				prev.LastUpload = c.Upload
				prev.LastDownload = c.Download
				prev.LastObservedAt = frameTs
				prev.CumulativeUpload += deltaUp
				prev.CumulativeDownload += deltaDown

				e.sink.Emit(&types.CollectorEvent{
					Type:                types.EventConnectionDelta,
					Timestamp:           frameTs,
					ConnectionID:        id,
					Process:             c.Metadata.Process,
					ProcessPath:         c.Metadata.ProcessPath,
					Host:                c.Metadata.Host,
					DestinationIP:       c.Metadata.DestinationIP,
					DestinationPort:     c.Metadata.DestinationPort,
					Rule:                c.Rule,
					RulePayload:         c.RulePayload,
					Chains:              c.Chains,
					Route:               prev.Route,
					AttributionClass:    prev.AttributionClass,
					DeltaUpload:         deltaUp,
					DeltaDownload:       deltaDown,
					CumulativeUpload:    prev.CumulativeUpload,
					CumulativeDownload:  prev.CumulativeDownload,
					AttributionInterval: []string{gapStartStr, frameTsStr},
					Precision:           "interval_only",
				})
			} else {
				// 分类 2: 恢复后首次出现的连接 (Baseline 归属，杜绝恢复瞬间爆炸)
				route := attribution.ClassifyRoute(c.Chains)
				attrClass := attribution.ClassifyInitialAttribution(&c)

				e.activeMap[id] = &ActiveConnectionState{
					Snapshot:        c,
					FirstObservedAt: frameTs,
					LastObservedAt:  frameTs,
					LastUpload:      c.Upload,
					LastDownload:    c.Download,
					Route:           route,
					AttributionClass: attrClass,
				}

				e.sink.Emit(&types.CollectorEvent{
					Type:               types.EventConnectionBootstrap,
					Timestamp:          frameTs,
					ConnectionID:       id,
					Process:            c.Metadata.Process,
					ProcessPath:        c.Metadata.ProcessPath,
					Host:               c.Metadata.Host,
					DestinationIP:      c.Metadata.DestinationIP,
					DestinationPort:    c.Metadata.DestinationPort,
					Rule:               c.Rule,
					RulePayload:        c.RulePayload,
					Chains:             c.Chains,
					Route:              route,
					AttributionClass:   attrClass,
					DeltaUpload:        0,
					DeltaDownload:      0,
					CumulativeUpload:   c.Upload,
					CumulativeDownload: c.Download,
					Precision:          "gap_post_baseline",
				})
			}
		}

		// 分类 3: 在 Gap 期间消失的连接
		for id, prev := range e.activeMap {
			if _, exists := currMap[id]; !exists {
				e.sink.Emit(&types.CollectorEvent{
					Type:                   types.EventConnectionDisappeared,
					Timestamp:              frameTs,
					ConnectionID:           id,
					Process:                prev.Snapshot.Metadata.Process,
					Host:                   prev.Snapshot.Metadata.Host,
					PossibleUnobservedTail: true,
					Details: map[string]any{
						"disappearedDuringGap": true,
					},
				})
				delete(e.activeMap, id)
			}
		}

		e.prevUploadTotal = payload.UploadTotal
		e.prevDownloadTotal = payload.DownloadTotal
		e.lastHealthyTimestamp = frameTs
		e.lastHealthyTimeString = frameTsStr
		e.isFirstFrameAfterReconnect = false
		e.sessionState = types.SessionHealthy
		return nil
	}

	// 4. 稳态快照帧处理 (Steady-State Frame Processing)
	currMap := make(map[string]types.ConnectionSnapshot, len(payload.Connections))
	for _, c := range payload.Connections {
		currMap[c.ID] = c
	}

	var frameAttributedUpload int64
	var frameAttributedDownload int64

	// a. 更新存活连接与处理新增连接
	for id, c := range currMap {
		if prev, exists := e.activeMap[id]; exists {
			deltaUp := c.Upload - prev.LastUpload
			deltaDown := c.Download - prev.LastDownload

			// Per-connection 计数器回退防护 (严禁静默 clamp)
			if deltaUp < 0 || deltaDown < 0 {
				e.sink.Emit(&types.CollectorEvent{
					Type:         types.EventCollectorHealth,
					Timestamp:    frameTs,
					ConnectionID: id,
					Details: map[string]any{
						"issue":          "connection_counter_regression",
						"prevUpload":     prev.LastUpload,
						"currUpload":     c.Upload,
						"prevDownload":   prev.LastDownload,
						"currDownload":   c.Download,
					},
				})
				prev.LastUpload = c.Upload
				prev.LastDownload = c.Download
				continue
			}

			prev.LastUpload = c.Upload
			prev.LastDownload = c.Download
			prev.LastObservedAt = frameTs
			prev.CumulativeUpload += deltaUp
			prev.CumulativeDownload += deltaDown

			frameAttributedUpload += deltaUp
			frameAttributedDownload += deltaDown

			e.sink.Emit(&types.CollectorEvent{
				Type:               types.EventConnectionDelta,
				Timestamp:          frameTs,
				ConnectionID:       id,
				Process:            c.Metadata.Process,
				ProcessPath:        c.Metadata.ProcessPath,
				Host:               c.Metadata.Host,
				DestinationIP:      c.Metadata.DestinationIP,
				DestinationPort:    c.Metadata.DestinationPort,
				Rule:               c.Rule,
				RulePayload:        c.RulePayload,
				Chains:             c.Chains,
				Route:              prev.Route,
				AttributionClass:   prev.AttributionClass,
				DeltaUpload:        deltaUp,
				DeltaDownload:      deltaDown,
				CumulativeUpload:   prev.CumulativeUpload,
				CumulativeDownload: prev.CumulativeDownload,
			})
		} else {
			// 稳态首次出现的连接
			route := attribution.ClassifyRoute(c.Chains)
			attrClass := attribution.ClassifyInitialAttribution(&c)

			deltaUp := c.Upload
			deltaDown := c.Download

			state := &ActiveConnectionState{
				Snapshot:           c,
				FirstObservedAt:    frameTs,
				LastObservedAt:     frameTs,
				LastUpload:         c.Upload,
				LastDownload:       c.Download,
				CumulativeUpload:   deltaUp,
				CumulativeDownload: deltaDown,
				Route:              route,
				AttributionClass:   attrClass,
			}
			e.activeMap[id] = state

			frameAttributedUpload += deltaUp
			frameAttributedDownload += deltaDown

			e.sink.Emit(&types.CollectorEvent{
				Type:               types.EventConnectionNew,
				Timestamp:          frameTs,
				ConnectionID:       id,
				Process:            c.Metadata.Process,
				ProcessPath:        c.Metadata.ProcessPath,
				Host:               c.Metadata.Host,
				DestinationIP:      c.Metadata.DestinationIP,
				DestinationPort:    c.Metadata.DestinationPort,
				Rule:               c.Rule,
				RulePayload:        c.RulePayload,
				Chains:             c.Chains,
				Route:              route,
				AttributionClass:   attrClass,
				DeltaUpload:        deltaUp,
				DeltaDownload:      deltaDown,
				CumulativeUpload:   deltaUp,
				CumulativeDownload: deltaDown,
			})
		}
	}

	// b. 消失连接检测 (Disappearance Detection)
	for id, prev := range e.activeMap {
		if _, exists := currMap[id]; !exists {
			e.sink.Emit(&types.CollectorEvent{
				Type:                   types.EventConnectionDisappeared,
				Timestamp:              frameTs,
				ConnectionID:           id,
				Process:                prev.Snapshot.Metadata.Process,
				Host:                   prev.Snapshot.Metadata.Host,
				PossibleUnobservedTail: true,
			})
			delete(e.activeMap, id)
		}
	}

	// c. 残差与全局计数器对齐
	globalUpDelta := payload.UploadTotal - e.prevUploadTotal
	globalDownDelta := payload.DownloadTotal - e.prevDownloadTotal

	if globalUpDelta >= 0 && globalDownDelta >= 0 {
		residualUp := globalUpDelta - frameAttributedUpload
		residualDown := globalDownDelta - frameAttributedDownload

		e.sink.Emit(&types.CollectorEvent{
			Type:      types.EventSamplingResidual,
			Timestamp: frameTs,
			Details: map[string]any{
				"globalUploadDelta":     globalUpDelta,
				"globalDownloadDelta":   globalDownDelta,
				"uniqueObservedUpload":  frameAttributedUpload,
				"uniqueObservedDownload": frameAttributedDownload,
				"residualUpload":        residualUp,
				"residualDownload":      residualDown,
			},
		})
	}

	e.prevUploadTotal = payload.UploadTotal
	e.prevDownloadTotal = payload.DownloadTotal
	e.lastHealthyTimestamp = frameTs
	e.lastHealthyTimeString = frameTsStr
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
