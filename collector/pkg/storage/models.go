package storage

import (
	"errors"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

var (
	ErrProjectionContractViolation = errors.New("projection contract violation")
	ErrNoCompletedAccountingRun   = errors.New("no completed accounting run available")
	ErrAccountingInvariantBroken   = errors.New("accounting invariant violation")
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
	SessionID    string `json:"sessionId"`
	EpochID      int    `json:"epochId"`
	ConnectionID string `json:"connectionId"`

	MihomoStart           string     `json:"mihomoStart,omitempty"`
	FirstObservedAt       time.Time  `json:"firstObservedAt"`
	LastObservedAt        time.Time  `json:"lastObservedAt"`
	DisappearedObservedAt *time.Time `json:"disappearedObservedAt,omitempty"`

	// Observation Lifecycle 语义字段 (E1)
	ObservationEndedAt    *time.Time `json:"observationEndedAt,omitempty"`
	ObservationEndReason  string     `json:"observationEndReason,omitempty"`
	ObservationEndEventID string     `json:"observationEndEventId,omitempty"`
	ObservationActive     bool       `json:"observationActive"`

	State                  string `json:"state"` // "active", "disappeared_from_snapshot" (保留兼容)
	PreexistingAtStart     bool   `json:"preexistingAtStart"`
	PossibleUnobservedTail bool   `json:"possibleUnobservedTail"`
	StartClassification    string `json:"startClassification,omitempty"`

	Metadata       types.RawMetadata `json:"metadata"`
	Rule           string            `json:"rule"`
	RulePayload    string            `json:"rulePayload"`
	Chains         []string          `json:"chains"`
	ProviderChains []string          `json:"providerChains"`

	Route                   types.RouteType        `json:"route"`
	LatestAttributionClass  types.AttributionClass `json:"latestAttributionClass"`
	QualityFlags            types.QualityFlags     `json:"qualityFlags"`
	RelayEvidence           map[string]any         `json:"relayEvidence,omitempty"`

	BaselineUploadCounter       int64 `json:"baselineUploadCounter"`
	BaselineDownloadCounter     int64 `json:"baselineDownloadCounter"`
	LastObservedUploadCounter   int64 `json:"lastObservedUploadCounter"`
	LastObservedDownloadCounter int64 `json:"lastObservedDownloadCounter"`
	MonitoredUploadTotal        int64 `json:"monitoredUploadTotal"`
	MonitoredDownloadTotal      int64 `json:"monitoredDownloadTotal"`
}

// ConnectionTrafficRecord 对应 connection_traffic 时序表投影
type ConnectionTrafficRecord struct {
	EventID       string `json:"eventId"`
	SessionID     string `json:"sessionId"`
	EpochID       int    `json:"epochId"`
	FrameSequence int64  `json:"frameSequence"`
	EventSequence int64  `json:"eventSequence"`
	ConnectionID  string `json:"connectionId"`

	ObservedAt    time.Time  `json:"observedAt"`
	IntervalStart *time.Time `json:"intervalStart,omitempty"`
	IntervalEnd   *time.Time `json:"intervalEnd,omitempty"`
	Precision     string     `json:"precision"`

	DeltaUpload   int64 `json:"deltaUpload"`
	DeltaDownload int64 `json:"deltaDownload"`

	ObservedUploadCounter   int64 `json:"observedUploadCounter"`
	ObservedDownloadCounter int64 `json:"observedDownloadCounter"`
	MonitoredUploadTotal    int64 `json:"monitoredUploadTotal"`
	MonitoredDownloadTotal  int64 `json:"monitoredDownloadTotal"`
}

// MonitoringGapRecord 对应 monitoring_gaps 表
type MonitoringGapRecord struct {
	GapID        string `json:"gapId"`
	Source       string `json:"source"` // "controller_stream", "collector_session_boundary"
	SessionID    string `json:"sessionId,omitempty"`
	OpenEventID  string `json:"openEventId,omitempty"`
	CloseEventID string `json:"closeEventId,omitempty"`

	StartedAt  time.Time  `json:"startedAt"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
	DurationMs *int64     `json:"durationMs,omitempty"`

	Reason                   string `json:"reason"`
	GlobalGapUploadDelta     *int64 `json:"globalGapUploadDelta,omitempty"`
	GlobalGapDownloadDelta   *int64 `json:"globalGapDownloadDelta,omitempty"`
	PhysicalDeltaUnavailable bool   `json:"physicalDeltaUnavailable"`

	Precision string    `json:"precision"`
	CreatedAt time.Time `json:"createdAt"`
}

// ResidualRecord 对应 residual_intervals 表
type ResidualRecord struct {
	EventID   string    `json:"eventId"`
	SessionID string    `json:"sessionId"`
	EpochID   int       `json:"epochId"`
	ObservedAt time.Time `json:"observedAt"`

	GlobalUploadDelta      int64 `json:"globalUploadDelta"`
	GlobalDownloadDelta    int64 `json:"globalDownloadDelta"`
	UniqueObservedUpload   int64 `json:"uniqueObservedUpload"`
	UniqueObservedDownload int64 `json:"uniqueObservedDownload"`
	ResidualUpload         int64 `json:"residualUpload"`
	ResidualDownload       int64 `json:"residualDownload"`

	DerivationVersion string    `json:"derivationVersion"`
	CreatedAt         time.Time `json:"createdAt"`
}

// HealthRecord 对应 collector_health 表
type HealthRecord struct {
	EventID      string         `json:"eventId"`
	SessionID    string         `json:"sessionId"`
	EpochID      int            `json:"epochId"`
	ObservedAt   time.Time      `json:"observedAt"`
	ConnectionID string         `json:"connectionId,omitempty"`
	Issue        string         `json:"issue"`
	Details      map[string]any `json:"details"`
	CreatedAt    time.Time      `json:"createdAt"`
}

// ConnectionFilter 用于多条件过滤连接
type ConnectionFilter struct {
	StartTime     *time.Time
	EndTime       *time.Time
	Route         types.RouteType
	Process       string
	Host          string
	DestinationIP string
	Network       string
	Limit         int
	Offset        int
}

// -------------------------------------------------------------
// E2 / E3: Reconciled Accounting Models
// -------------------------------------------------------------

type AccountingRunStatus string

const (
	AccountingRunRunning   AccountingRunStatus = "running"
	AccountingRunCompleted AccountingRunStatus = "completed"
	AccountingRunFailed    AccountingRunStatus = "failed"
)

type AccountingRunRecord struct {
	RunID                   string              `json:"runId"`
	AlgorithmVersion        string              `json:"algorithmVersion"`
	StartedAt               time.Time           `json:"startedAt"`
	CompletedAt             *time.Time          `json:"completedAt,omitempty"`
	Status                  AccountingRunStatus `json:"status"`
	SourceJournalEventCount int64               `json:"sourceJournalEventCount"`
	SourceBoundaryJSON      string              `json:"sourceBoundaryJson"`
	Notes                   string              `json:"notes,omitempty"`
}

type RelayRelationStatus string

const (
	RelayConfirmed RelayRelationStatus = "confirmed"
	RelayAmbiguous RelayRelationStatus = "ambiguous"
	RelayUnpaired  RelayRelationStatus = "unpaired"
)

type RelayRelationRecord struct {
	RunID                 string              `json:"runId"`
	CandidateSessionID    string              `json:"candidateSessionId"`
	CandidateEpochID      int                 `json:"candidateEpochId"`
	CandidateConnectionID string              `json:"candidateConnectionId"`
	LogicalSessionID      string              `json:"logicalSessionId,omitempty"`
	LogicalEpochID        int                 `json:"logicalEpochId,omitempty"`
	LogicalConnectionID   string              `json:"logicalConnectionId,omitempty"`
	Status                RelayRelationStatus `json:"status"`
	EvidenceJSON          string              `json:"evidenceJson"`
	DerivationVersion     string              `json:"derivationVersion"`
}

type AccountingClass string

const (
	ClassUnique                   AccountingClass = "unique"
	ClassConfirmedRelayDuplicate  AccountingClass = "confirmed_relay_duplicate"
	ClassAmbiguousRelay           AccountingClass = "ambiguous_relay"
	ClassMissingAttribution       AccountingClass = "missing_attribution"
)

type AccountedTrafficRecord struct {
	RunID          string          `json:"runId"`
	SourceEventID  string          `json:"sourceEventId"`
	SessionID      string          `json:"sessionId"`
	EpochID        int             `json:"epochId"`
	ConnectionID   string          `json:"connectionId"`
	ObservedAt     time.Time       `json:"observedAt"`
	IntervalStart  *time.Time      `json:"intervalStart,omitempty"`
	IntervalEnd    *time.Time      `json:"intervalEnd,omitempty"`
	Precision      string          `json:"precision"`
	Route          types.RouteType `json:"route"`
	RawUpload      int64           `json:"rawUpload"`
	RawDownload    int64           `json:"rawDownload"`
	AccountedUpload int64          `json:"accountedUpload"`
	AccountedDownload int64        `json:"accountedDownload"`
	AccountingClass AccountingClass `json:"accountingClass"`
	Process        string          `json:"process,omitempty"`
	ProcessPath    string          `json:"processPath,omitempty"`
	Host           string          `json:"host,omitempty"`
	SniffHost      string          `json:"sniffHost,omitempty"`
	DestinationIP  string          `json:"destinationIp,omitempty"`
	Network        string          `json:"network,omitempty"`
	Rule           string          `json:"rule,omitempty"`
	RulePayload    string          `json:"rulePayload,omitempty"`
	FinalProxy     string          `json:"finalProxy,omitempty"`
	TopPolicyGroup string          `json:"topPolicyGroup,omitempty"`
	DimensionDerivationVersion string `json:"dimensionDerivationVersion"`
}

// -------------------------------------------------------------
// E5: Hourly Materialized Aggregate Model
// -------------------------------------------------------------

type UsageHourlyDimensionRecord struct {
	RunID                  string          `json:"runId"`
	BucketStart            time.Time       `json:"bucketStart"`
	DimensionType          string          `json:"dimensionType"` // total, process, host, destination_ip, rule, rule_payload, final_proxy, top_policy_group, network
	DimensionKey           string          `json:"dimensionKey"`
	Route                  types.RouteType `json:"route"`
	UploadBytes            int64           `json:"uploadBytes"`
	DownloadBytes          int64           `json:"downloadBytes"`
	ConnectionCount        int64           `json:"connectionCount"`
	ExactUploadBytes       int64           `json:"exactUploadBytes"`
	ExactDownloadBytes     int64           `json:"exactDownloadBytes"`
	EstimatedUploadBytes   int64           `json:"estimatedUploadBytes"`
	EstimatedDownloadBytes int64           `json:"estimatedDownloadBytes"`
}

// -------------------------------------------------------------
// E4 / E6 / E7: Query, Summary & Coverage Models
// -------------------------------------------------------------

type UsageSummary struct {
	RawObservedUpload            int64     `json:"rawObservedUpload"`
	RawObservedDownload          int64     `json:"rawObservedDownload"`
	UniqueObservedUpload         int64     `json:"uniqueObservedUpload"`
	UniqueObservedDownload       int64     `json:"uniqueObservedDownload"`

	ProxyUpload                  int64     `json:"proxyUpload"`
	ProxyDownload                int64     `json:"proxyDownload"`
	DirectUpload                 int64     `json:"directUpload"`
	DirectDownload               int64     `json:"directDownload"`
	RejectUpload                 int64     `json:"rejectUpload"`
	RejectDownload               int64     `json:"rejectDownload"`
	UnknownRouteUpload           int64     `json:"unknownRouteUpload"`
	UnknownRouteDownload         int64     `json:"unknownRouteDownload"`

	MissingAttributionUpload     int64     `json:"missingAttributionUpload"`
	MissingAttributionDownload   int64     `json:"missingAttributionDownload"`
	AmbiguousRelayUpload         int64     `json:"ambiguousRelayUpload"`
	AmbiguousRelayDownload       int64     `json:"ambiguousRelayDownload"`

	SamplingResidualUpload       int64     `json:"samplingResidualUpload"`
	SamplingResidualDownload     int64     `json:"samplingResidualDownload"`

	ControllerGapPhysicalUpload  int64     `json:"controllerGapPhysicalUpload"`
	ControllerGapPhysicalDownload int64    `json:"controllerGapPhysicalDownload"`

	Coverage                     *CoverageSummary `json:"coverage,omitempty"`
	AccountingVersion            string           `json:"accountingVersion"`
}

type MergedGap struct {
	Source     string    `json:"source"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
	DurationMs int64     `json:"durationMs"`
	Reason     string    `json:"reason"`
}

type CoverageSummary struct {
	RequestedStart            *time.Time  `json:"requestedStart,omitempty"`
	RequestedEnd              *time.Time  `json:"requestedEnd,omitempty"`
	KnownScopeStart           *time.Time  `json:"knownScopeStart,omitempty"`
	EffectiveScopeStart       *time.Time  `json:"effectiveScopeStart,omitempty"`
	EffectiveScopeEnd         *time.Time  `json:"effectiveScopeEnd,omitempty"`

	CoveredDurationMs         int64       `json:"coveredDurationMs"`
	UncoveredDurationMs       int64       `json:"uncoveredDurationMs"`
	OutsideKnownScopeMs       int64       `json:"outsideKnownScopeMs"`

	CoverageRatio             *float64    `json:"coverageRatio,omitempty"` // nil if outside known scope
	ControllerGapDurationMs   int64       `json:"controllerGapDurationMs"`
	CollectorOfflineDurationMs int64      `json:"collectorOfflineDurationMs"`

	MergedGaps                []MergedGap `json:"mergedGaps"`
}

type TopDimensionItem struct {
	Key                    string          `json:"key"`
	Route                  types.RouteType `json:"route,omitempty"`
	UploadBytes            int64           `json:"uploadBytes"`
	DownloadBytes          int64           `json:"downloadBytes"`
	TotalBytes             int64           `json:"totalBytes"`
	ConnectionCount        int64           `json:"connectionCount"`
	ExactUploadBytes       int64           `json:"exactUploadBytes"`
	ExactDownloadBytes     int64           `json:"exactDownloadBytes"`
	EstimatedUploadBytes   int64           `json:"estimatedUploadBytes"`
	EstimatedDownloadBytes int64           `json:"estimatedDownloadBytes"`
}

type AnalyticsFilter struct {
	StartTime *time.Time
	EndTime   *time.Time
	Route     types.RouteType // "PROXY", "DIRECT", "REJECT", "" (ALL)
	Limit     int
}
