package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

func (s *Server) handleIntelligenceFindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.intelligenceSvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Audit intelligence service is unavailable")
		return
	}

	from, to, err := parseTimeFilter(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_TIMESTAMP", err.Error())
		return
	}
	if from == nil || to == nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_TIME_RANGE", "Both 'from' and 'to' timestamps are required")
		return
	}
	if !to.After(*from) {
		s.writeError(w, http.StatusBadRequest, "INVALID_TIME_RANGE", "The 'to' timestamp must be later than 'from'")
		return
	}

	limit := storage.AuditFindingsDefaultLimit
	if raw := r.URL.Query().Get("limitPerKind"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > storage.AuditFindingsMaxLimit {
			s.writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limitPerKind must be between 1 and 50")
			return
		}
		limit = parsed
	}

	result, err := s.intelligenceSvc.ListFindings(r.Context(), storage.AuditFindingFilter{
		StartTime: from, EndTime: to, LimitPerKind: limit,
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNoCompletedAccountingRun):
			s.writeError(w, http.StatusNotFound, "NO_COMPLETED_ACCOUNTING_RUN", "No completed accounting run found in database")
		case errors.Is(err, storage.ErrInvalidAuditFindingRange):
			s.writeError(w, http.StatusBadRequest, "INVALID_TIME_RANGE", "The selected time range is invalid")
		case errors.Is(err, storage.ErrInvalidAuditFindingLimit):
			s.writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limitPerKind must be between 1 and 50")
		default:
			s.logInternalError("ListFindings failed", err)
			s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to retrieve audit findings")
		}
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func parseRequiredTemporalBoundary(r *http.Request, name string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, fmtError("missing '" + name + "' timestamp")
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return nil, fmtError("invalid '" + name + "' timestamp: must be RFC3339")
	}
	utc := t.UTC()
	return &utc, nil
}

func (s *Server) handleIntelligenceProcessChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.intelligenceSvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Audit intelligence service is unavailable")
		return
	}

	values := make(map[string]*time.Time, 4)
	for _, name := range []string{"baselineFrom", "baselineTo", "recentFrom", "recentTo"} {
		value, err := parseRequiredTemporalBoundary(r, name)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "INVALID_TIMESTAMP", err.Error())
			return
		}
		values[name] = value
	}

	limit := storage.ProcessChangesDefaultLimit
	if raw := r.URL.Query().Get("limitPerKind"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > storage.ProcessChangesMaxLimit {
			s.writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limitPerKind must be between 1 and 50")
			return
		}
		limit = parsed
	}

	result, err := s.intelligenceSvc.ListProcessChanges(r.Context(), storage.ProcessChangeFilter{
		BaselineFrom: values["baselineFrom"],
		BaselineTo:   values["baselineTo"],
		RecentFrom:   values["recentFrom"],
		RecentTo:     values["recentTo"],
		LimitPerKind: limit,
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNoCompletedAccountingRun):
			s.writeError(w, http.StatusNotFound, "NO_COMPLETED_ACCOUNTING_RUN", "No completed accounting run found in database")
		case errors.Is(err, storage.ErrInvalidProcessChangeRange):
			s.writeError(w, http.StatusBadRequest, "INVALID_TIME_RANGE", "The baseline window must end at or before the recent window begins; both windows must be UTC-hour aligned and at least one hour long")
		case errors.Is(err, storage.ErrInvalidProcessChangeLimit):
			s.writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limitPerKind must be between 1 and 50")
		default:
			s.logInternalError("ListProcessChanges failed", err)
			s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to retrieve process comparison")
		}
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleIntelligenceTemporalFindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	if s.intelligenceSvc == nil {
		s.writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "Audit intelligence service is unavailable")
		return
	}

	values := make(map[string]*time.Time, 4)
	for _, name := range []string{"baselineFrom", "baselineTo", "recentFrom", "recentTo"} {
		value, err := parseRequiredTemporalBoundary(r, name)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "INVALID_TIMESTAMP", err.Error())
			return
		}
		values[name] = value
	}

	limit := storage.TemporalFindingsDefaultLimit
	if raw := r.URL.Query().Get("limitPerKind"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > storage.TemporalFindingsMaxLimit {
			s.writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limitPerKind must be between 1 and 50")
			return
		}
		limit = parsed
	}

	result, err := s.intelligenceSvc.ListTemporalFindings(r.Context(), storage.TemporalFindingsFilter{
		BaselineFrom: values["baselineFrom"],
		BaselineTo:   values["baselineTo"],
		RecentFrom:   values["recentFrom"],
		RecentTo:     values["recentTo"],
		LimitPerKind: limit,
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNoCompletedAccountingRun):
			s.writeError(w, http.StatusNotFound, "NO_COMPLETED_ACCOUNTING_RUN", "No completed accounting run found in database")
		case errors.Is(err, storage.ErrInvalidTemporalComparisonRange):
			s.writeError(w, http.StatusBadRequest, "INVALID_TIME_RANGE", "The baseline window must end at or before the recent window begins; both windows must be UTC-hour aligned and at least one hour long")
		case errors.Is(err, storage.ErrInvalidTemporalComparisonLimit):
			s.writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limitPerKind must be between 1 and 50")
		default:
			s.logInternalError("ListTemporalFindings failed", err)
			s.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "Failed to retrieve temporal findings")
		}
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
