package storage

import (
	"errors"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

var (
	ErrProjectionContractViolation = errors.New("projection contract violation")
)

// SessionStatus 表示 Collector 运行会话状态
type SessionStatus string

const (
	SessionStatusRunning     SessionStatus = "running"
	SessionStatusClosedClean SessionStatus = "closed_clean"
	SessionStatusInterrupted SessionStatus = "interrupted"
)

// CollectorSessionRecord 对应 collector_sessions 表
type CollectorSessionRecord struct {
	SessionID         string        `json:"sessionId"`
	StartedAt         time.Time     `json:"startedAt"`
	EndedAt           *time.Time    `json:"endedAt,omitempty"`
	LastEventAt       *time.Time    `json:"lastEventAt,omitempty"`
	LastFrameSequence int64         `json:"lastFrameSequence"`
	Status            SessionStatus `json:"status"`
	CollectorVersion  string        `json:"collectorVersion,omitempty"`
	CreatedAt         time.Time     `json:"createdAt"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}

// ConnectionRecord 对应 connections 维度表投影
type ConnectionRecord struct {
	SessionID string `json:"sessionId"`
	EpochID   int    `json:"epochId"`
	ConnectionID string `json:"connectionId"`

	MihomoStart string `json:"mihomoStart,omitempty"`
	FirstObservedAt time.Time `json:"firstObservedAt"`
	LastObservedAt time.Time `json:"lastObservedAt"`
	DisappearedObservedAt *time.Time `json:"disappearedObservedAt,omitempty"`

	State string `json:"state"` // "active", "disappeared_from_snapshot"
	PreexistingAtStart bool `json:"preexistingAtStart"`
	PossibleUnobservedTail bool `json:"possibleUnobservedTail"`
	StartClassification string `json:"startClassification,omitempty"`

	Metadata types.RawMetadata `json:"metadata"`
	Rule string `json:"rule"`
	RulePayload string `json:"rulePayload"`
	Chains []string `json:"chains"`
	ProviderChains []string `json:"providerChains"`

	Route types.RouteType `json:"route"`
	LatestAttributionClass types.AttributionClass `json:"latestAttributionClass"`
	QualityFlags types.QualityFlags `json:"qualityFlags"`
	RelayEvidence map[string]any `json:"relayEvidence,omitempty"`

	BaselineUploadCounter int64 `json:"baselineUploadCounter"`
	BaselineDownloadCounter int64 `json:"baselineDownloadCounter"`
	LastObservedUploadCounter int64 `json:"lastObservedUploadCounter"`
	LastObservedDownloadCounter int64 `json:"lastObservedDownloadCounter"`
	MonitoredUploadTotal int64 `json:"monitoredUploadTotal"`
	MonitoredDownloadTotal int64 `json:"monitoredDownloadTotal"`
}

// ConnectionTrafficRecord 对应 connection_traffic 时序表投影
type ConnectionTrafficRecord struct {
	EventID string `json:"eventId"`
	SessionID string `json:"sessionId"`
	EpochID int `json:"epochId"`
	FrameSequence int64 `json:"frameSequence"`
	EventSequence int64 `json:"eventSequence"`
	ConnectionID string `json:"connectionId"`

	ObservedAt time.Time `json:"observedAt"`
	IntervalStart *time.Time `json:"intervalStart,omitempty"`
	IntervalEnd *time.Time `json:"intervalEnd,omitempty"`
	Precision string `json:"precision"`

	DeltaUpload int64 `json:"deltaUpload"`
	DeltaDownload int64 `json:"deltaDownload"`

	ObservedUploadCounter int64 `json:"observedUploadCounter"`
	ObservedDownloadCounter int64 `json:"observedDownloadCounter"`
	MonitoredUploadTotal int64 `json:"monitoredUploadTotal"`
	MonitoredDownloadTotal int64 `json:"monitoredDownloadTotal"`
}

// MonitoringGapRecord 对应 monitoring_gaps 表
type MonitoringGapRecord struct {
	GapID string `json:"gapId"`
	Source string `json:"source"` // "controller_stream", "collector_session_boundary"
	SessionID string `json:"sessionId,omitempty"`
	OpenEventID string `json:"openEventId,omitempty"`
	CloseEventID string `json:"closeEventId,omitempty"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt *time.Time `json:"endedAt,omitempty"`
	DurationMs *int64 `json:"durationMs,omitempty"`

	Reason string `json:"reason"`
	GlobalGapUploadDelta *int64 `json:"globalGapUploadDelta,omitempty"`
	GlobalGapDownloadDelta *int64 `json:"globalGapDownloadDelta,omitempty"`
	PhysicalDeltaUnavailable bool `json:"physicalDeltaUnavailable"`

	Precision string `json:"precision"`
	CreatedAt time.Time `json:"createdAt"`
}

// ResidualRecord 对应 residual_intervals 表
type ResidualRecord struct {
	EventID string `json:"eventId"`
	SessionID string `json:"sessionId"`
	EpochID int `json:"epochId"`
	ObservedAt time.Time `json:"observedAt"`

	GlobalUploadDelta int64 `json:"globalUploadDelta"`
	GlobalDownloadDelta int64 `json:"globalDownloadDelta"`
	UniqueObservedUpload int64 `json:"uniqueObservedUpload"`
	UniqueObservedDownload int64 `json:"uniqueObservedDownload"`
	ResidualUpload int64 `json:"residualUpload"`
	ResidualDownload int64 `json:"residualDownload"`

	DerivationVersion string `json:"derivationVersion"`
	CreatedAt time.Time `json:"createdAt"`
}

// HealthRecord 对应 collector_health 表
type HealthRecord struct {
	EventID string `json:"eventId"`
	SessionID string `json:"sessionId"`
	EpochID int `json:"epochId"`
	ObservedAt time.Time `json:"observedAt"`
	ConnectionID string `json:"connectionId,omitempty"`
	Issue string `json:"issue"`
	Details map[string]any `json:"details"`
	CreatedAt time.Time `json:"createdAt"`
}

// ConnectionFilter 用于多条件过滤连接
type ConnectionFilter struct {
	StartTime *time.Time
	EndTime *time.Time
	Route types.RouteType
	Process string
	Host string
	DestinationIP string
	Network string
	Limit int
	Offset int
}
