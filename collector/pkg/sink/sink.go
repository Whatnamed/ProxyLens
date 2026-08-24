package sink

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// EventSink 是 Collector 输出事件的目标接口
type EventSink interface {
	Emit(event *types.CollectorEvent) error
	Close() error
}

// MemorySink 在内存中存储事件并提供聚合统计
type MemorySink struct {
	mu                   sync.RWMutex
	events               []*types.CollectorEvent
	totalFrames          int64
	totalObservations    int64
	bootstrapCount       int64
	newIdsCount          int64
	updatesCount         int64
	disappearancesCount  int64
	epochBreaksCount     int64
	gapsCount            int64
	attributedUpload     int64
	attributedDownload   int64
	relayDuplicatesCount int64
	activeMap            map[string]bool
}

// NewMemorySink 创建 MemorySink 实例
func NewMemorySink() *MemorySink {
	return &MemorySink{
		events:    make([]*types.CollectorEvent, 0, 1024),
		activeMap: make(map[string]bool),
	}
}

// Emit 接收并统计事件
func (s *MemorySink) Emit(event *types.CollectorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, event)

	switch event.Type {
	case types.EventConnectionBootstrap:
		s.bootstrapCount++
		s.totalObservations++
		s.activeMap[event.ConnectionID] = true
	case types.EventConnectionNew:
		s.newIdsCount++
		s.totalObservations++
		s.attributedUpload += event.DeltaUpload
		s.attributedDownload += event.DeltaDownload
		s.activeMap[event.ConnectionID] = true
	case types.EventConnectionDelta:
		s.updatesCount++
		s.totalObservations++
		s.attributedUpload += event.DeltaUpload
		s.attributedDownload += event.DeltaDownload
		s.activeMap[event.ConnectionID] = true
	case types.EventConnectionDisappeared:
		s.disappearancesCount++
		delete(s.activeMap, event.ConnectionID)
	case types.EventCounterEpochBreak:
		s.epochBreaksCount++
		s.activeMap = make(map[string]bool)
	case types.EventMonitoringGapClosed:
		s.gapsCount++
	}

	if event.AttributionClass == types.ClassConfirmedRelayDuplicate {
		s.relayDuplicatesCount++
	}

	return nil
}

// IncrementFrames 记录处理的帧数
func (s *MemorySink) IncrementFrames() {
	s.mu.Lock()
	s.totalFrames++
	s.mu.Unlock()
}

// GetEvents 获取所有事件快照
func (s *MemorySink) GetEvents() []*types.CollectorEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]*types.CollectorEvent, len(s.events))
	copy(res, s.events)
	return res
}

// GetSummary 返回聚合统计快照
func (s *MemorySink) GetSummary() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summaryStr := fmt.Sprintf(
		"frames:%d|obs:%d|boot:%d|new:%d|upd:%d|dis:%d|epoch:%d|up:%d|down:%d|active:%d",
		s.totalFrames, s.totalObservations, s.bootstrapCount, s.newIdsCount, s.updatesCount,
		s.disappearancesCount, s.epochBreaksCount, s.attributedUpload, s.attributedDownload, len(s.activeMap),
	)
	hasher := sha256.New()
	hasher.Write([]byte(summaryStr))
	checksum := hex.EncodeToString(hasher.Sum(nil))

	return map[string]any{
		"totalFrames":          s.totalFrames,
		"totalObservations":    s.totalObservations,
		"bootstrapCount":       s.bootstrapCount,
		"newIdsCount":          s.newIdsCount,
		"updatesCount":         s.updatesCount,
		"disappearancesCount":  s.disappearancesCount,
		"epochBreaksCount":     s.epochBreaksCount,
		"gapsCount":            s.gapsCount,
		"attributedUpload":     s.attributedUpload,
		"attributedDownload":   s.attributedDownload,
		"relayDuplicatesCount": s.relayDuplicatesCount,
		"activeConnections":    len(s.activeMap),
		"semanticChecksum":     checksum,
	}
}

// Close 实现 EventSink 接口
func (s *MemorySink) Close() error {
	return nil
}
