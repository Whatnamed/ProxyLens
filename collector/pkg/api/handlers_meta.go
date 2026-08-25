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

	var schemaVer sql.NullInt64
	_ = s.db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations;").Scan(&schemaVer)

	dbState := "READY"
	if !schemaVer.Valid || schemaVer.Int64 == 0 {
		dbState = "UNINITIALIZED"
	} else if schemaVer.Int64 > int64(maxBinary) {
		dbState = "INCOMPATIBLE"
	}

	resp := MetaResponse{
		APIVersion:             "v1",
		AppVersion:             s.appVersion,
		DBState:                dbState,
		SchemaVersion:          int(schemaVer.Int64),
		MaxBinarySchemaVersion: maxBinary,
	}

	if s.querySvc != nil && dbState == "READY" {
		latestSess, _ := s.querySvc.GetLatestSession(ctx)
		resp.LatestCollectorSession = latestSess
	}

	if s.analyticsSvc != nil && dbState == "READY" {
		latestRun, _ := s.analyticsSvc.GetLatestCompletedAccountingRun(ctx)
		resp.LatestAccountingRun = latestRun

		freshness, _ := s.analyticsSvc.GetAccountingFreshness(ctx)
		resp.Freshness = freshness
	}

	s.writeJSON(w, http.StatusOK, resp)
}
