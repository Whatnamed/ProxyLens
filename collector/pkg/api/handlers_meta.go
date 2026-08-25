package api

import (
	"database/sql"
	"net/http"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	ctx := r.Context()
	maxBinary := storage.GetMaxBinaryMigrationVersion()

	if s.db == nil {
		s.writeJSON(w, http.StatusOK, MetaResponse{
			APIVersion:             "v1",
			AppVersion:             s.appVersion,
			DBState:                "UNAVAILABLE",
			SchemaVersion:          0,
			MaxBinarySchemaVersion: maxBinary,
		})
		return
	}

	dbState := "READY"
	schemaVer := 0
	var ver sql.NullInt64

	if err := s.db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations;").Scan(&ver); err != nil {
		s.logInternalError("handleMeta failed to query schema_migrations", err)
		dbState = "UNAVAILABLE"
	} else if !ver.Valid || ver.Int64 <= 0 {
		dbState = "UNINITIALIZED"
	} else {
		schemaVer = int(ver.Int64)
		if schemaVer > maxBinary {
			dbState = "INCOMPATIBLE"
		}
	}

	resp := MetaResponse{
		APIVersion:             "v1",
		AppVersion:             s.appVersion,
		DBState:                dbState,
		SchemaVersion:          schemaVer,
		MaxBinarySchemaVersion: maxBinary,
	}

	if s.querySvc != nil && dbState == "READY" {
		if latestSess, err := s.querySvc.GetLatestSession(ctx); err != nil {
			s.logInternalError("handleMeta failed to query latest session", err)
		} else {
			resp.LatestCollectorSession = latestSess
		}
	}

	if s.analyticsSvc != nil && dbState == "READY" {
		if latestRun, err := s.analyticsSvc.GetLatestCompletedAccountingRun(ctx); err != nil {
			s.logInternalError("handleMeta failed to query latest accounting run", err)
		} else {
			resp.LatestAccountingRun = latestRun
		}

		if freshness, err := s.analyticsSvc.GetAccountingFreshness(ctx); err != nil {
			s.logInternalError("handleMeta failed to query freshness", err)
		} else {
			resp.Freshness = freshness
		}
	}

	s.writeJSON(w, http.StatusOK, resp)
}
