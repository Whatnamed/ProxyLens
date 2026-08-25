package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func createTestDBWithSeedData(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "proxylens-api-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(dir, "api-test.db")

	ctx := context.Background()
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, "sess-api-test", "v1.0.0-test")
	if err != nil {
		t.Fatalf("OpenSQLiteSink failed: %v", err)
	}

	t0 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)

	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ev-api-1", SessionID: "sess-api-test", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c-api-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "chrome.exe", ProcessPath: "C:\\Program Files\\Google\\Chrome\\chrome.exe",
			Host: "google.com", DestinationIP: "142.250.190.46", DestinationPort: "443", Network: "tcp",
		},
		Rule: "DomainSuffix", RulePayload: "google.com",
		Chains: []string{"Node-HK-01", "ProxyGroup"},
		DeltaUpload: 1000, DeltaDownload: 5000,
	})

	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ev-api-2", SessionID: "sess-api-test", EpochID: 1, FrameSequence: 2, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(10 * time.Minute), ConnectionID: "c-api-2",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "curl.exe", Host: "example.org", DestinationIP: "93.184.216.34", DestinationPort: "80", Network: "tcp",
		},
		Rule: "DirectRule", RulePayload: "Direct",
		DeltaUpload: 200, DeltaDownload: 800,
	})

	_ = sink.EndSession(ctx, "sess-api-test", storage.SessionStatusClosedClean)
	_ = sink.Close()

	// 运行一次 RebuildAccounting 建立核算基准
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if _, err := storage.RebuildAccounting(ctx, db, "api test run"); err != nil {
		t.Fatalf("RebuildAccounting failed: %v", err)
	}

	return dbPath, func() {
		_ = os.RemoveAll(dir)
	}
}

func TestAPIServerAuthCORSAndEndpoints(t *testing.T) {
	dbPath, cleanup := createTestDBWithSeedData(t)
	defer cleanup()

	ctx := context.Background()
	roDB, err := storage.OpenReadOnlyDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnlyDB failed: %v", err)
	}
	defer roDB.Close()

	testToken := "test-secret-token-32-chars-length"
	server, err := NewServer(ServerConfig{
		DB:         roDB,
		DBPath:     dbPath,
		Token:      testToken,
		AppVersion: "0.7.0-test",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	mux := http.NewServeMux()
	server.registerRoutes(mux)
	handler := server.AuthAndCORSMiddleware(mux)

	// 1. Healthz 免鉴权
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected healthz to return 200, got %d", w.Code)
	}

	// 2. 缺失 Bearer Token -> 401
	req = httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected missing token to return 401, got %d", w.Code)
	}

	// 3. 错误 Bearer Token -> 401
	req = httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected wrong token to return 401, got %d", w.Code)
	}

	// 4. 恶意 Origin CORS -> 403
	req = httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Origin", "http://evil-attacker.com")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected evil origin to return 403, got %d", w.Code)
	}

	// 5. 合法 Tauri Origin -> 200 并携带 CORS Header
	req = httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Origin", "http://localhost:5173")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected valid origin with token to return 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("Expected CORS header for allowed origin, got: %s", w.Header().Get("Access-Control-Allow-Origin"))
	}

	var metaResp MetaResponse
	if err := json.NewDecoder(w.Body).Decode(&metaResp); err != nil {
		t.Fatalf("Failed to decode meta response: %v", err)
	}
	if metaResp.APIVersion != "v1" || metaResp.DBState != "READY" || metaResp.SchemaVersion <= 0 {
		t.Errorf("Meta response mismatch: %+v", metaResp)
	}

	// 6. /api/v1/analytics/summary
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/summary?route=PROXY", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Summary request failed with %d: %s", w.Code, w.Body.String())
	}
	var sum storage.UsageSummary
	if err := json.NewDecoder(w.Body).Decode(&sum); err != nil {
		t.Fatalf("Failed to decode summary: %v", err)
	}
	if sum.ProxyUpload != 1000 || sum.ProxyDownload != 5000 {
		t.Errorf("Summary values mismatch: %+v", sum)
	}

	// 7. /api/v1/analytics/top/processes
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/top/processes?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Top processes failed with %d", w.Code)
	}

	// 8. 非法 Timestamp -> 400
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/summary?from=invalid-time", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected invalid timestamp to return 400, got %d", w.Code)
	}

	// 9. 非法 Route -> 400
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/summary?route=INVALID_ROUTE", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected invalid route to return 400, got %d", w.Code)
	}

	// 10. /api/v1/connections 分页与过滤
	req = httptest.NewRequest(http.MethodGet, "/api/v1/connections?limit=1", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Connections list failed: %d", w.Code)
	}
	var connsList ConnectionsListResponse
	if err := json.NewDecoder(w.Body).Decode(&connsList); err != nil {
		t.Fatalf("Failed to decode conns list: %v", err)
	}
	if len(connsList.Items) != 1 || !connsList.HasMore {
		t.Errorf("Expected limit=1 with hasMore=true, got len=%d, hasMore=%v", len(connsList.Items), connsList.HasMore)
	}

	// 11. /api/v1/connections/{sessionId}/{epochId}/{connectionId}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/connections/sess-api-test/1/c-api-1", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Connection detail failed: %d (%s)", w.Code, w.Body.String())
	}
	var connDetail ConnectionDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&connDetail); err != nil {
		t.Fatalf("Failed to decode conn detail: %v", err)
	}
	if connDetail.Connection == nil || connDetail.Connection.ConnectionID != "c-api-1" {
		t.Errorf("Expected c-api-1, got %+v", connDetail)
	}
	if connDetail.Accounting == nil || connDetail.Accounting.Process != "chrome.exe" {
		t.Errorf("Expected accounting chrome.exe, got %+v", connDetail.Accounting)
	}

	// 12. 缺失连接 -> 404
	req = httptest.NewRequest(http.MethodGet, "/api/v1/connections/sess-api-test/1/non-existent-conn", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected non-existent conn to return 404, got %d", w.Code)
	}

	// 13. /api/v1/connections/{sessionId}/{epochId}/{connectionId}/traffic
	req = httptest.NewRequest(http.MethodGet, "/api/v1/connections/sess-api-test/1/c-api-1/traffic", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Connection traffic failed: %d", w.Code)
	}
	var connTraffic ConnectionTrafficResponse
	if err := json.NewDecoder(w.Body).Decode(&connTraffic); err != nil {
		t.Fatalf("Failed to decode conn traffic: %v", err)
	}
	if len(connTraffic.Traffic) == 0 {
		t.Errorf("Expected traffic records, got 0")
	}

	// 14. /api/v1/coverage
	req = httptest.NewRequest(http.MethodGet, "/api/v1/coverage", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Coverage failed: %d", w.Code)
	}
}
