package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func parseTimeFilter(r *http.Request) (*time.Time, *time.Time, error) {
	q := r.URL.Query()
	var startTime, endTime *time.Time

	if fromStr := q.Get("from"); fromStr != "" {
		t, err := time.Parse(time.RFC3339Nano, fromStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, fromStr)
		}
		if err != nil {
			return nil, nil, fmtError("invalid 'from' timestamp: must be RFC3339")
		}
		tUTC := t.UTC()
		startTime = &tUTC
	}

	if toStr := q.Get("to"); toStr != "" {
		t, err := time.Parse(time.RFC3339Nano, toStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, toStr)
		}
		if err != nil {
			return nil, nil, fmtError("invalid 'to' timestamp: must be RFC3339")
		}
		tUTC := t.UTC()
		endTime = &tUTC
	}

	return startTime, endTime, nil
}

func parseRouteFilter(r *http.Request) (types.RouteType, error) {
	routeStr := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("route")))
	if routeStr == "" || routeStr == "ALL" {
		return "", nil
	}
	switch routeStr {
	case string(types.RouteProxy):
		return types.RouteProxy, nil
	case string(types.RouteDirect):
		return types.RouteDirect, nil
	case string(types.RouteReject):
		return types.RouteReject, nil
	default:
		return "", fmtError("invalid route: must be PROXY, DIRECT, REJECT, or ALL")
	}
}

func parseLimit(r *http.Request, defaultLimit, maxLimit int) int {
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		return defaultLimit
	}
	l, err := strconv.Atoi(limitStr)
	if err != nil || l <= 0 {
		return defaultLimit
	}
	if l > maxLimit {
		return maxLimit
	}
	return l
}

func fmtError(msg string) error {
	return errors.New(msg)
}

func (s *Server) handleAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.analyticsSvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Analytics service is unavailable")
		return
	}

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

	filter := storage.AnalyticsFilter{
		StartTime: from,
		EndTime:   to,
		Route:     route,
	}

	summary, err := s.analyticsSvc.GetUsageSummary(r.Context(), filter)
	if err != nil {
		if errors.Is(err, storage.ErrNoCompletedAccountingRun) {
			s.writeError(w, http.StatusNotFound, "NO_COMPLETED_ACCOUNTING_RUN", "No completed accounting run found in database")
			return
		}
		s.logInternalError("GetUsageSummary failed", err)
		s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to retrieve usage summary")
		return
	}

	s.writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleTopProcesses(w http.ResponseWriter, r *http.Request) {
	s.handleTopQuery(w, r, "GetTopProcesses", func(ctx context.Context, f storage.AnalyticsFilter) ([]storage.TopDimensionItem, error) {
		return s.analyticsSvc.GetTopProcesses(ctx, f)
	})
}

func (s *Server) handleTopHosts(w http.ResponseWriter, r *http.Request) {
	s.handleTopQuery(w, r, "GetTopHosts", func(ctx context.Context, f storage.AnalyticsFilter) ([]storage.TopDimensionItem, error) {
		return s.analyticsSvc.GetTopHosts(ctx, f)
	})
}

func (s *Server) handleTopRules(w http.ResponseWriter, r *http.Request) {
	s.handleTopQuery(w, r, "GetTopRules", func(ctx context.Context, f storage.AnalyticsFilter) ([]storage.TopDimensionItem, error) {
		return s.analyticsSvc.GetTopRules(ctx, f)
	})
}

func (s *Server) handleTopFinalProxies(w http.ResponseWriter, r *http.Request) {
	s.handleTopQuery(w, r, "GetTopFinalProxies", func(ctx context.Context, f storage.AnalyticsFilter) ([]storage.TopDimensionItem, error) {
		return s.analyticsSvc.GetTopFinalProxies(ctx, f)
	})
}

func (s *Server) handleProtocols(w http.ResponseWriter, r *http.Request) {
	s.handleTopQuery(w, r, "GetProtocolBreakdown", func(ctx context.Context, f storage.AnalyticsFilter) ([]storage.TopDimensionItem, error) {
		return s.analyticsSvc.GetProtocolBreakdown(ctx, f)
	})
}

func (s *Server) handleTopQuery(w http.ResponseWriter, r *http.Request, queryName string, fn func(ctx context.Context, f storage.AnalyticsFilter) ([]storage.TopDimensionItem, error)) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.analyticsSvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Analytics service is unavailable")
		return
	}

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

	limit := parseLimit(r, 20, 100)

	filter := storage.AnalyticsFilter{
		StartTime: from,
		EndTime:   to,
		Route:     route,
		Limit:     limit,
	}

	items, err := fn(r.Context(), filter)
	if err != nil {
		if errors.Is(err, storage.ErrNoCompletedAccountingRun) {
			s.writeError(w, http.StatusNotFound, "NO_COMPLETED_ACCOUNTING_RUN", "No completed accounting run found in database")
			return
		}
		s.logInternalError(queryName+" failed", err)
		s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to retrieve top dimension statistics")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"limit": limit,
	})
}

func (s *Server) handleCoverage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.analyticsSvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Analytics service is unavailable")
		return
	}

	from, to, err := parseTimeFilter(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_TIMESTAMP", err.Error())
		return
	}

	coverage, err := s.analyticsSvc.GetCoverage(r.Context(), from, to)
	if err != nil {
		s.logInternalError("GetCoverage failed", err)
		s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to calculate monitoring coverage")
		return
	}

	s.writeJSON(w, http.StatusOK, coverage)
}
