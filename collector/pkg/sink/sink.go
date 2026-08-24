package sink

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// EventSink 是 Collector 输出事件的目标接口
type EventSink interface {
	Emit(event *types.CollectorEvent) error
	Close() error
}

// ProductionStatsSink 是生产路径默认使用的安全 Sink，不无限保存历史事件，杜绝内存泄漏
type ProductionStatsSink struct {
	mu                   sync.RWMutex
	totalFrames          atomic.Int64
	totalObservations    atomic.Int64
	bootstrapCount       atomic.Int64
	newIdsCount          atomic.Int64
	updatesCount         atomic.Int64
	disappearancesCount  atomic.Int64
	epochBreaksCount     atomic.Int64
	gapsCount            atomic.Int64
	healthAlertsCount    atomic.Int64
	attributedUpload     atomic.Int64
	attributedDownload   atomic.Int64
	relayDuplicatesCount atomic.Int64
	activeMap            map[string]bool
	hasher               sync.Mutex
	semanticHashBuilder  []string
}

// NewProductionStatsSink 创建 ProductionStatsSink
func NewProductionStatsSink() *ProductionStatsSink {
	return &ProductionStatsSink{
		activeMap: make(map[string]bool),
	}
}

// Emit 增量统计事件
func (s *ProductionStatsSink) Emit(event *types.CollectorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch event.Type {
	case types.EventConnectionBootstrap:
		s.bootstrapCount.Add(1)
		s.totalObservations.Add(1)
		s.activeMap[event.ConnectionID] = true
	case types.EventConnectionNew:
		s.newIdsCount.Add(1)
		s.totalObservations.Add(1)
		s.attributedUpload.Add(event.DeltaUpload)
		s.attributedDownload.Add(event.DeltaDownload)
		s.activeMap[event.ConnectionID] = true
	case types.EventConnectionDelta:
		s.updatesCount.Add(1)
		s.totalObservations.Add(1)
		s.attributedUpload.Add(event.DeltaUpload)
		s.attributedDownload.Add(event.DeltaDownload)
		s.activeMap[event.ConnectionID] = true
	case types.EventConnectionDisappeared:
		s.disappearancesCount.Add(1)
		delete(s.activeMap, event.ConnectionID)
	case types.EventCounterEpochBreak:
		s.epochBreaksCount.Add(1)
		s.activeMap = make(map[string]bool)
	case types.EventMonitoringGapClosed:
		s.gapsCount.Add(1)
	case types.EventCollectorHealth:
		s.healthAlertsCount.Add(1)
	}

	if event.AttributionClass == types.ClassConfirmedRelayDuplicate {
		s.relayDuplicatesCount.Add(1)
	}

	return nil
}

// IncrementFrames 记录帧数
func (s *ProductionStatsSink) IncrementFrames() {
	s.totalFrames.Add(1)
}

// GetSummary 获取聚合摘要
func (s *ProductionStatsSink) GetSummary() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeCount := len(s.activeMap)
	summaryStr := fmt.Sprintf(
		"frames:%d|obs:%d|boot:%d|new:%d|upd:%d|dis:%d|epoch:%d|up:%d|down:%d|active:%d",
		s.totalFrames.Load(), s.totalObservations.Load(), s.bootstrapCount.Load(),
		s.newIdsCount.Load(), s.updatesCount.Load(), s.disappearancesCount.Load(),
		s.epochBreaksCount.Load(), s.attributedUpload.Load(), s.attributedDownload.Load(),
		activeCount,
	)
	hasher := sha256.New()
	hasher.Write([]byte(summaryStr))
	checksum := hex.EncodeToString(hasher.Sum(nil))

	return map[string]any{
		"totalFrames":          s.totalFrames.Load(),
		"totalObservations":    s.totalObservations.Load(),
		"bootstrapCount":       s.bootstrapCount.Load(),
		"newIdsCount":          s.newIdsCount.Load(),
		"updatesCount":         s.updatesCount.Load(),
		"disappearancesCount":  s.disappearancesCount.Load(),
		"epochBreaksCount":     s.epochBreaksCount.Load(),
		"gapsCount":            s.gapsCount.Load(),
		"healthAlertsCount":    s.healthAlertsCount.Load(),
		"attributedUpload":     s.attributedUpload.Load(),
		"attributedDownload":   s.attributedDownload.Load(),
		"relayDuplicatesCount": s.relayDuplicatesCount.Load(),
		"activeConnections":    activeCount,
		"semanticChecksum":     checksum,
	}
}

// Close 实现 EventSink
func (s *ProductionStatsSink) Close() error {
	return nil
}

// MemorySink 在内存中存储事件切片（仅用于单测与离线回放）
type MemorySink struct {
	mu          sync.RWMutex
	events      []*types.CollectorEvent
	statsSink   *ProductionStatsSink
}

// NewMemorySink 创建 MemorySink
func NewMemorySink() *MemorySink {
	return &MemorySink{
		events:    make([]*types.CollectorEvent, 0, 512),
		statsSink: NewProductionStatsSink(),
	}
}

// Emit 存储事件并更新统计
func (s *MemorySink) Emit(event *types.CollectorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return s.statsSink.Emit(event)
}

// IncrementFrames 记录帧数
func (s *MemorySink) IncrementFrames() {
	s.statsSink.IncrementFrames()
}

// GetEvents 获取事件快照
func (s *MemorySink) GetEvents() []*types.CollectorEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]*types.CollectorEvent, len(s.events))
	copy(res, s.events)
	return res
}

// GetSummary 返回聚合统计
func (s *MemorySink) GetSummary() map[string]any {
	return s.statsSink.GetSummary()
}

// Close 实现 EventSink
func (s *MemorySink) Close() error {
	return nil
}

// ValidationJSONLSink 在验证模式下将事件流式写入 JSONL 文件
type ValidationJSONLSink struct {
	mu        sync.Mutex
	file      *os.File
	encoder   *json.Encoder
	statsSink *ProductionStatsSink
}

// NewValidationJSONLSink 创建 ValidationJSONLSink
func NewValidationJSONLSink(path string) (*ValidationJSONLSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	return &ValidationJSONLSink{
		file:      f,
		encoder:   json.NewEncoder(f),
		statsSink: NewProductionStatsSink(),
	}, nil
}

// Emit 写入 JSONL 并更新统计
func (s *ValidationJSONLSink) Emit(event *types.CollectorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.encoder.Encode(event); err != nil {
		return err
	}
	return s.statsSink.Emit(event)
}

// IncrementFrames 记录帧数
func (s *ValidationJSONLSink) IncrementFrames() {
	s.statsSink.IncrementFrames()
}

// GetSummary 获取聚合摘要
func (s *ValidationJSONLSink) GetSummary() map[string]any {
	return s.statsSink.GetSummary()
}

// Close 关闭文件
func (s *ValidationJSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// FailingSink 用于故障注入测试
type FailingSink struct {
	FailAfterCount int
	CurrentCount   int
	InnerSink      EventSink
}

// Emit 模拟写入失败
func (s *FailingSink) Emit(event *types.CollectorEvent) error {
	s.CurrentCount++
	if s.FailAfterCount > 0 && s.CurrentCount >= s.FailAfterCount {
		return fmt.Errorf("injected sink failure at event %d", s.CurrentCount)
	}
	if s.InnerSink != nil {
		return s.InnerSink.Emit(event)
	}
	return nil
}

// Close 关闭
func (s *FailingSink) Close() error {
	if s.InnerSink != nil {
		return s.InnerSink.Close()
	}
	return nil
}
