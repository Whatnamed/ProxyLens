package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// SessionState 表示 Collector 运行状态
type SessionState string

const (
	SessionStarting         SessionState = "Starting"
	SessionConnecting       SessionState = "Connecting"
	SessionBootstrap        SessionState = "Bootstrap"
	SessionHealthy          SessionState = "Healthy"
	SessionReconnectBackoff SessionState = "ReconnectBackoff"
	SessionRecovering       SessionState = "Recovering"
	SessionEpochBoundary    SessionState = "EpochBoundary"
	SessionStopped          SessionState = "Stopped"
)

// RouteType 表示派生的出站路由类型
type RouteType string

const (
	RouteDirect  RouteType = "DIRECT"
	RouteProxy   RouteType = "PROXY"
	RouteReject  RouteType = "REJECT"
	RouteUnknown RouteType = "UNKNOWN"
)

// AttributionClass 表示分层归因类别
type AttributionClass string

const (
	ClassKnownApplication         AttributionClass = "known_application"
	ClassUnpairedMissingAttr      AttributionClass = "unpaired_missing_attribution"
	ClassRelayCandidate           AttributionClass = "relay_candidate"
	ClassConfirmedRelayDuplicate  AttributionClass = "confirmed_relay_duplicate"
)

// EventType 表示 Collector 输出的标准事件类型
type EventType string

const (
	EventConnectionBootstrap        EventType = "ConnectionBootstrap"
	EventConnectionNew              EventType = "ConnectionNew"
	EventConnectionDelta            EventType = "ConnectionDelta"
	EventConnectionDisappeared      EventType = "ConnectionDisappeared"
	EventConnectionMetadataUpdated  EventType = "ConnectionMetadataUpdated"
	EventRelayClassificationChanged EventType = "RelayClassificationChanged"
	EventSamplingResidual           EventType = "SamplingResidual"
	EventMonitoringGapOpened        EventType = "MonitoringGapOpened"
	EventMonitoringGapClosed        EventType = "MonitoringGapClosed"
	EventCounterEpochBreak          EventType = "CounterEpochBreak"
	EventCollectorHealth            EventType = "CollectorHealth"
)

// QualityFlags 细化记录元数据字段完整度
type QualityFlags struct {
	MissingProcess     bool `json:"missingProcess"`
	MissingProcessPath bool `json:"missingProcessPath"`
	MissingHost        bool `json:"missingHost"`
	IPOnly             bool `json:"ipOnly"`
	MissingRule        bool `json:"missingRule"`
	MissingChain       bool `json:"missingChain"`
}

// RawMetadata 映射 Mihomo /connections metadata 完整原始字段
type RawMetadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationIP     string `json:"destinationIP"`
	RemoteDestination string `json:"remoteDestination"`
	DestinationPort   string `json:"destinationPort"`
	Host              string `json:"host"`
	SniffHost         string `json:"sniffHost"`
	DnsMode           string `json:"dnsMode"`
	Process           string `json:"process"`
	ProcessPath       string `json:"processPath"`
	SpecialProxy      string `json:"specialProxy"`
	SpecialRules      string `json:"specialRules"`
	InboundUser       string `json:"inboundUser"`
	InboundName       string `json:"inboundName"`
	InboundPort       string `json:"inboundPort"`
}

// DeriveQualityFlags 提取元数据质量标记
func (m *RawMetadata) DeriveQualityFlags(rule string, chains []string) QualityFlags {
	return QualityFlags{
		MissingProcess:     m.Process == "",
		MissingProcessPath: m.ProcessPath == "",
		MissingHost:        m.Host == "",
		IPOnly:             m.Host == "" && m.DestinationIP != "",
		MissingRule:        rule == "",
		MissingChain:       len(chains) == 0,
	}
}

// ConnectionSnapshot 映射单个连接快照的完整原始字段
type ConnectionSnapshot struct {
	ID             string      `json:"id"`
	Metadata       RawMetadata `json:"metadata"`
	Upload         int64       `json:"upload"`
	Download       int64       `json:"download"`
	Start          string      `json:"start"`
	Chains         []string    `json:"chains"`
	Rule           string      `json:"rule"`
	RulePayload    string      `json:"rulePayload"`
	ProviderChains []string    `json:"providerChains,omitempty"`
}

// ConnectionSnapshotPayload 映射 /connections 根 JSON 载荷
type ConnectionSnapshotPayload struct {
	DownloadTotal int64                `json:"downloadTotal"`
	UploadTotal   int64                `json:"uploadTotal"`
	Connections   []ConnectionSnapshot `json:"connections"`
	Memory        int64                `json:"memory,omitempty"`
}

// ConnectionSnapshotFrame 包含接收时间戳和载荷
type ConnectionSnapshotFrame struct {
	ReceivedAt string                    `json:"receivedAt"`
	Frame      ConnectionSnapshotPayload `json:"frame"`
}

// GetPayload 获取规范化载荷
func (f *ConnectionSnapshotFrame) GetPayload() ConnectionSnapshotPayload {
	return f.Frame
}

// TrafficPayload 映射 /traffic 根 JSON 载荷
type TrafficPayload struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// IngestItemKind 定义有序摄取通道中项的类型
type IngestItemKind string

const (
	ItemFrame           IngestItemKind = "Frame"
	ItemGapOpened       IngestItemKind = "GapOpened"
	ItemCollectorHealth IngestItemKind = "CollectorHealth"
	ItemQueueOverload   IngestItemKind = "QueueOverload"
)

// IngestItem 是输入给 StateEngine 的单一有序摄取项
type IngestItem struct {
	Kind         IngestItemKind
	Timestamp    time.Time
	Frame        *ConnectionSnapshotFrame
	HealthIssue  string
	Details      map[string]any
	SequenceNum  int64
}

// CollectorEvent 是 Collector 输出到 Storage / Sink 的标准化强类型事件
type CollectorEvent struct {
	EventID                    string                 `json:"eventId"`
	SessionID                  string                 `json:"sessionId"`
	EpochID                    int                    `json:"epochId"`
	FrameSequence              int64                  `json:"frameSequence"`
	EventSequence              int64                  `json:"eventSequence"`
	Timestamp                  time.Time              `json:"timestamp"`
	Type                       EventType              `json:"type"`
	ConnectionID               string                 `json:"connectionId,omitempty"`
	Metadata                   RawMetadata            `json:"metadata,omitempty"`
	QualityFlags               QualityFlags           `json:"qualityFlags"`
	MihomoStart                string                 `json:"mihomoStart,omitempty"`
	Rule                       string                 `json:"rule,omitempty"`
	RulePayload                string                 `json:"rulePayload,omitempty"`
	Chains                     []string               `json:"chains,omitempty"`
	ProviderChains             []string               `json:"providerChains,omitempty"`
	Route                      RouteType              `json:"route,omitempty"`
	AttributionClass           AttributionClass       `json:"attributionClass,omitempty"`
	ObservedUploadCounter      int64                  `json:"observedUploadCounter"`
	ObservedDownloadCounter    int64                  `json:"observedDownloadCounter"`
	DeltaUpload                int64                  `json:"deltaUpload"`
	DeltaDownload              int64                  `json:"deltaDownload"`
	MonitoredCumulativeUpload  int64                  `json:"monitoredCumulativeUpload"`
	MonitoredCumulativeDownload int64                 `json:"monitoredCumulativeDownload"`
	BaselineUploadCounter      int64                  `json:"baselineUploadCounter"`
	BaselineDownloadCounter    int64                  `json:"baselineDownloadCounter"`
	PreexistingAtStart         bool                   `json:"preexistingAtStart,omitempty"`
	PossibleUnobservedTail     bool                   `json:"possibleUnobservedTail,omitempty"`
	AttributionInterval        []string               `json:"attributionInterval,omitempty"`
	Precision                  string                 `json:"precision,omitempty"`
	Details                    map[string]any         `json:"details,omitempty"`
}

// GenerateDeterministicEventID 计算事件的确定性哈希 ID
func (e *CollectorEvent) GenerateDeterministicEventID() {
	raw := fmt.Sprintf("%s|%d|%d|%d|%s|%s|%d|%d",
		e.SessionID, e.EpochID, e.FrameSequence, e.EventSequence,
		e.Type, e.ConnectionID, e.DeltaUpload, e.DeltaDownload,
	)
	hasher := sha256.New()
	hasher.Write([]byte(raw))
	e.EventID = hex.EncodeToString(hasher.Sum(nil))
}
