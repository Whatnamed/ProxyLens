package api

import (
	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

// ErrorResponse 标准结构化错误响应
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// MetaResponse 系统与会话元数据响应
type MetaResponse struct {
	APIVersion              string                          `json:"apiVersion"`
	AppVersion              string                          `json:"appVersion"`
	DBState                 string                          `json:"dbState"` // READY, UNAVAILABLE, INCOMPATIBLE
	SchemaVersion           int                             `json:"schemaVersion"`
	MaxBinarySchemaVersion  int                             `json:"maxBinarySchemaVersion"`
	LatestCollectorSession  *storage.CollectorSessionRecord `json:"latestCollectorSession,omitempty"`
	LatestAccountingRun     *storage.AccountingRunRecord    `json:"latestAccountingRun,omitempty"`
	Freshness               *storage.AccountingFreshness     `json:"freshness,omitempty"`
}

// ConnectionsListResponse 分页连接列表响应
type ConnectionsListResponse struct {
	Items   []*storage.ConnectionRecord `json:"items"`
	Limit   int                         `json:"limit"`
	Offset  int                         `json:"offset"`
	HasMore bool                        `json:"hasMore"`
}

// ConnectionDetailResponse 单条连接详情与审计证据
type ConnectionDetailResponse struct {
	Connection *storage.ConnectionRecord `json:"connection"`
	Accounting *storage.AccountedTrafficRecord `json:"accounting,omitempty"`
}

// ConnectionTrafficResponse 单条连接的流量时序帧
type ConnectionTrafficResponse struct {
	ConnectionID string                            `json:"connectionId"`
	Traffic      []*storage.ConnectionTrafficRecord `json:"traffic"`
}
