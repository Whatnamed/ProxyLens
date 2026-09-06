package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

func TestProcessChangesAPIContractAndCoverageStatus(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "temporal-api.db")
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO collector_sessions (
			session_id, started_at, ended_at, last_event_at, last_frame_sequence, status,
			collector_version, created_at, updated_at, last_heartbeat_at, heartbeat_interval_ms
		) VALUES ('sess-api-temporal', '2026-09-03T23:00:00Z', '2026-09-05T04:00:00Z', '2026-09-05T03:00:00Z', 1, 'closed_clean', 'test', '2026-09-03T23:00:00Z', '2026-09-05T04:00:00Z', '2026-09-05T03:00:00Z', 5000);
		INSERT INTO accounting_runs (
			run_id, algorithm_version, started_at, completed_at, status,
			source_journal_event_count, source_journal_sequence_max, source_boundary_json, notes
		) VALUES ('run-api-temporal', 'legacy-v1', '2026-09-05T04:00:00Z', '2026-09-05T04:00:00Z', 'completed', 4, 4, '{}', 'api temporal');
		INSERT INTO usage_hourly_dimensions (
			run_id, bucket_start, dimension_type, dimension_key, route,
			upload_bytes, download_bytes, connection_count,
			exact_upload_bytes, exact_download_bytes, estimated_upload_bytes, estimated_download_bytes
		) VALUES
		('run-api-temporal', '2026-09-04T00:00:00Z', 'process', 'api-new.exe', 'DIRECT', 1, 1, 1, 1, 1, 0, 0),
		('run-api-temporal', '2026-09-05T00:00:00Z', 'process', 'api-new.exe', 'PROXY', 10, 90, 1, 10, 90, 0, 0),
		('run-api-temporal', '2026-09-04T01:00:00Z', 'process', 'api-growth.exe', 'PROXY', 10, 90, 1, 10, 90, 0, 0),
		('run-api-temporal', '2026-09-05T01:00:00Z', 'process', 'api-growth.exe', 'PROXY', 50, 450, 1, 50, 450, 0, 0);
	`)
	if err != nil {
		db.Close()
		t.Fatalf("seed temporal API DB: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close writer DB: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("temporary DB missing: %v", err)
	}

	roDB, err := storage.OpenReadOnlyDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnlyDB failed: %v", err)
	}
	defer roDB.Close()
	server, err := NewServer(ServerConfig{DB: roDB, DBPath: dbPath, Token: "temporal-api-token"})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	mux := http.NewServeMux()
	server.registerRoutes(mux)
	handler := server.AuthAndCORSMiddleware(mux)
	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer temporal-api-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	if response := call(http.MethodGet, "/api/v1/intelligence/process-changes"); response.Code != http.StatusBadRequest {
		t.Fatalf("missing boundary status=%d body=%s", response.Code, response.Body.String())
	}
	if response := call(http.MethodGet, "/api/v1/intelligence/process-changes?baselineFrom=2026-09-04T00:30:00Z&baselineTo=2026-09-04T02:00:00Z&recentFrom=2026-09-05T00:00:00Z&recentTo=2026-09-05T02:00:00Z"); response.Code != http.StatusBadRequest {
		t.Fatalf("unaligned boundary status=%d body=%s", response.Code, response.Body.String())
	}
	if response := call(http.MethodPost, "/api/v1/intelligence/process-changes?baselineFrom=2026-09-04T00:00:00Z&baselineTo=2026-09-04T02:00:00Z&recentFrom=2026-09-05T00:00:00Z&recentTo=2026-09-05T02:00:00Z"); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status=%d", response.Code)
	}

	valid := call(http.MethodGet, "/api/v1/intelligence/process-changes?baselineFrom=2026-09-04T00:00:00Z&baselineTo=2026-09-04T02:00:00Z&recentFrom=2026-09-05T00:00:00Z&recentTo=2026-09-05T02:00:00Z&limitPerKind=1")
	if valid.Code != http.StatusOK {
		t.Fatalf("valid status=%d body=%s", valid.Code, valid.Body.String())
	}
	var result storage.ProcessChangeResult
	if err := json.NewDecoder(valid.Body).Decode(&result); err != nil {
		t.Fatalf("decode temporal result: %v", err)
	}
	if result.Status != storage.ComparisonReady || result.LimitPerKind != 1 || len(result.Items) != 2 {
		t.Fatalf("unexpected temporal API result: %+v", result)
	}

	reversed := call(http.MethodGet, "/api/v1/intelligence/process-changes?baselineFrom=2026-09-05T00:00:00Z&baselineTo=2026-09-06T00:00:00Z&recentFrom=2026-09-01T00:00:00Z&recentTo=2026-09-02T00:00:00Z")
	if reversed.Code != http.StatusBadRequest {
		t.Fatalf("chronologically reversed comparison status=%d body=%s", reversed.Code, reversed.Body.String())
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/intelligence/process-changes?baselineFrom=2026-09-04T00:00:00Z&baselineTo=2026-09-04T02:00:00Z&recentFrom=2026-09-05T00:00:00Z&recentTo=2026-09-05T02:00:00Z", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status=%d", unauthenticated.Code)
	}
}
