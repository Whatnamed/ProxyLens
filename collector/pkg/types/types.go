package types

import "time"

// RouteType 表示派生路由类型
type RouteType string

const (
	RouteDirect  RouteType = "DIRECT"
	RouteProxy   RouteType = "PROXY"
	RouteReject  RouteType = "REJECT"
	RouteOther   RouteType = "OTHER"
	RouteUnknown RouteType = "UNKNOWN"
)

// AttributionClass 表示分层流量归因类别
type AttributionClass string

const (
	ClassKnownApplication        AttributionClass = "known_application"
	ClassUnpairedMissingAttr     AttributionClass = "unpaired_missing_attribution"
	ClassRelayCandidate          AttributionClass = "relay_candidate"
	ClassConfirmedRelayDuplicate AttributionClass = "confirmed_relay_duplicate"
	ClassOtherObservedUnique     AttributionClass = "other_observed_unique"
	ClassSamplingResidual        AttributionClass = "sampling_residual"
	ClassMonitoringGap           AttributionClass = "monitoring_gap"
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
	SessionStopped          SessionState = "Stopped"
)

// RawMetadata 映射 Mihomo /connections metadata 字段
type RawMetadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationIP     string `json:"destinationIP"`
	DestinationPort   string `json:"destinationPort"`
	InboundIP         string `json:"inboundIP"`
	InboundPort       string `json:"inboundPort"`
	InboundName       string `json:"inboundName"`
	InboundUser       string `json:"inboundUser"`
	Host              string `json:"host"`
	DnsMode           string `json:"dnsMode"`
	Process           string `json:"process"`
	ProcessPath       string `json:"processPath"`
	SpecialProxy      string `json:"specialProxy"`
	SpecialRules      string `json:"specialRules"`
	RemoteDestination string `json:"remoteDestination"`
	SniffHost         string `json:"sniffHost"`
}

// ConnectionSnapshot 映射单条连接的原始快照
type ConnectionSnapshot struct {
	ID             string      `json:"id"`
	Start          string      `json:"start"`
	Upload         int64       `json:"upload"`
	Download       int64       `json:"download"`
	Metadata       RawMetadata `json:"metadata"`
	Chains         []string    `json:"chains"`
	ProviderChains []string    `json:"providerChains"`
	Rule           string      `json:"rule"`
	RulePayload    string      `json:"rulePayload"`
}

// ConnectionSnapshotPayload 映射快照载荷
type ConnectionSnapshotPayload struct {
	DownloadTotal int64                `json:"downloadTotal"`
	UploadTotal   int64                `json:"uploadTotal"`
	Connections   []ConnectionSnapshot `json:"connections"`
}

// ConnectionSnapshotFrame 映射完整的单帧 WebSocket 消息
type ConnectionSnapshotFrame struct {
	ReceivedAt string                    `json:"receivedAt"`
	Frame      ConnectionSnapshotPayload `json:"frame"`
	Payload    ConnectionSnapshotPayload `json:"payload"`
}

// GetPayload 兼容 frame 与 payload 包装
func (f *ConnectionSnapshotFrame) GetPayload() ConnectionSnapshotPayload {
	if len(f.Frame.Connections) > 0 || f.Frame.UploadTotal > 0 || f.Frame.DownloadTotal > 0 {
		return f.Frame
	}
	return f.Payload
}

// TrafficRateFrame 映射 /traffic 实时速率帧
type TrafficRateFrame struct {
	ReceivedAt time.Time `json:"receivedAt"`
	Up         int64     `json:"up"`
	Down       int64     `json:"down"`
}

// ConnectionEventType 事件类型
type ConnectionEventType string

const (
	EventConnectionBootstrap    ConnectionEventType = "ConnectionBootstrap"
	EventConnectionNew          ConnectionEventType = "ConnectionNew"
	EventConnectionDelta        ConnectionEventType = "ConnectionDelta"
	EventConnectionDisappeared  ConnectionEventType = "ConnectionDisappeared"
	EventCounterEpochBreak      ConnectionEventType = "CounterEpochBreak"
	EventMonitoringGapClosed    ConnectionEventType = "MonitoringGapClosed"
	EventSamplingResidual       ConnectionEventType = "SamplingResidual"
	EventCollectorHealth        ConnectionEventType = "CollectorHealth"
)

// CollectorEvent 是 Collector 输出到 Sink 的标准化事件
type CollectorEvent struct {
	Type                   ConnectionEventType `json:"type"`
	Timestamp              time.Time           `json:"timestamp"`
	ConnectionID           string              `json:"connectionId,omitempty"`
	Process                string              `json:"process,omitempty"`
	ProcessPath            string              `json:"processPath,omitempty"`
	Host                   string              `json:"host,omitempty"`
	DestinationIP          string              `json:"destinationIP,omitempty"`
	DestinationPort        string              `json:"destinationPort,omitempty"`
	Rule                   string              `json:"rule,omitempty"`
	RulePayload            string              `json:"rulePayload,omitempty"`
	Chains                 []string            `json:"chains,omitempty"`
	Route                  RouteType           `json:"route,omitempty"`
	AttributionClass       AttributionClass    `json:"attributionClass,omitempty"`
	DeltaUpload            int64               `json:"deltaUpload,omitempty"`
	DeltaDownload          int64               `json:"deltaDownload,omitempty"`
	CumulativeUpload       int64               `json:"cumulativeUpload,omitempty"`
	CumulativeDownload     int64               `json:"cumulativeDownload,omitempty"`
	PreexistingAtStart     bool                `json:"preexistingAtStart,omitempty"`
	PossibleUnobservedTail bool                `json:"possibleUnobservedTail,omitempty"`
	AttributionInterval    []string            `json:"attributionInterval,omitempty"`
	Precision              string              `json:"precision,omitempty"`
	Details                map[string]any      `json:"details,omitempty"`
}
