package api

import (
	"errors"
	"net/http"
	"strconv"

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
