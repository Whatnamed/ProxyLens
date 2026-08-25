package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

func (s *Server) logInternalError(contextMsg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "[API INTERNAL ERROR] %s: %v\n", contextMsg, err)
	}
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.querySvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Query service is unavailable")
		return
	}

	q := r.URL.Query()
	from, to, err := parseTimeFilter(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_TIMESTAMP", err.Error())
		return
	}

	route, err := parseRouteFilter(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_ROUTE", err.Error())
		return
	}

	limit := parseLimit(r, 50, 200)
	offset := 0
	if offsetStr := q.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	filter := storage.ConnectionFilter{
		StartTime:     from,
		EndTime:       to,
		Route:         route,
		Process:       q.Get("process"),
		Host:          q.Get("host"),
		DestinationIP: q.Get("destinationIp"),
		Network:       q.Get("network"),
		Limit:         limit + 1, // 查询 limit+1 行用于判断 hasMore
		Offset:        offset,
	}

	items, err := s.querySvc.ListConnections(r.Context(), filter)
	if err != nil {
		s.logInternalError("ListConnections failed", err)
		s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to query connections")
		return
	}

	hasMore := false
	if len(items) > limit {
		hasMore = true
		items = items[:limit]
	}

	s.writeJSON(w, http.StatusOK, ConnectionsListResponse{
		Items:   items,
		Limit:   limit,
		Offset:  offset,
		HasMore: hasMore,
	})
}

func (s *Server) handleConnectionDetailRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.querySvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Query service is unavailable")
		return
	}

	// 路径格式:
	// /api/v1/connections/{sessionId}/{epochId}/{connectionId}
	// /api/v1/connections/{sessionId}/{epochId}/{connectionId}/traffic
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/connections/")
	parts := strings.Split(trimmed, "/")

	if len(parts) < 3 {
		s.writeError(w, http.StatusBadRequest, "INVALID_PATH", "Expected /api/v1/connections/{sessionId}/{epochId}/{connectionId}[/traffic]")
		return
	}

	sessionID := parts[0]
	epochID, err := strconv.Atoi(parts[1])
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_EPOCH_ID", "epochId must be an integer")
		return
	}
	connectionID := parts[2]

	if len(parts) == 4 && parts[3] == "traffic" {
		// 查询流量增量时序帧
		traffic, err := s.querySvc.ListConnectionTraffic(r.Context(), sessionID, epochID, connectionID)
		if err != nil {
			s.logInternalError("ListConnectionTraffic failed", err)
			s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to query connection traffic")
			return
		}
		s.writeJSON(w, http.StatusOK, ConnectionTrafficResponse{
			ConnectionID: connectionID,
			Traffic:      traffic,
		})
		return
	}

	if len(parts) == 3 {
		// 查询连接物理维度元数据
		conn, err := s.querySvc.GetConnection(r.Context(), sessionID, epochID, connectionID)
		if err != nil {
			if errors.Is(err, storage.ErrConnectionNotFound) {
				s.writeError(w, http.StatusNotFound, "CONNECTION_NOT_FOUND", "Connection not found")
				return
			}
			s.logInternalError("GetConnection failed", err)
			s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to get connection details")
			return
		}

		// 按三元组身份查询 Accounting Events 完整时序列表
		accEvents, err := s.querySvc.ListAccountedTrafficForConnection(r.Context(), sessionID, epochID, connectionID)
		if err != nil {
			s.logInternalError("ListAccountedTrafficForConnection failed", err)
		}

		// 聚合生成 Accounting Summary (如果存在事件)
		var summary *ConnectionAccountingSummary
		if len(accEvents) > 0 {
			var rawUp, rawDown, accUp, accDown int64
			latestEv := accEvents[len(accEvents)-1] // 最新的事件行提供最新元数据

			for _, ev := range accEvents {
				rawUp += ev.RawUpload
				rawDown += ev.RawDownload
				accUp += ev.AccountedUpload
				accDown += ev.AccountedDownload
			}

			summary = &ConnectionAccountingSummary{
				RunID:                  latestEv.RunID,
				AccountingClass:        latestEv.AccountingClass,
				Route:                  string(latestEv.Route),
				RawUploadTotal:         rawUp,
				RawDownloadTotal:       rawDown,
				AccountedUploadTotal:   accUp,
				AccountedDownloadTotal: accDown,
				LatestProcess:          latestEv.Process,
				LatestProcessPath:      latestEv.ProcessPath,
				LatestHost:             latestEv.Host,
				LatestSniffHost:        latestEv.SniffHost,
				LatestDestinationIP:    latestEv.DestinationIP,
				LatestNetwork:          latestEv.Network,
				LatestRule:             latestEv.Rule,
				LatestRulePayload:      latestEv.RulePayload,
				LatestFinalProxy:       latestEv.FinalProxy,
				LatestTopPolicyGroup:   latestEv.TopPolicyGroup,
			}
		}

		s.writeJSON(w, http.StatusOK, ConnectionDetailResponse{
			Connection:        conn,
			AccountingEvents:  accEvents,
			AccountingSummary: summary,
		})
		return
	}

	s.writeError(w, http.StatusNotFound, "NOT_FOUND", "Endpoint not found")
}
