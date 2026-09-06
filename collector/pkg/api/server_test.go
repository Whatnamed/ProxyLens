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

	t0 := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ev-api-1", SessionID: "sess-api-test", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "c-api-1",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "chrome.exe", ProcessPath: "C:\\Program Files\\Google\\Chrome\\chrome.exe",
			Host: "google.com", DestinationIP: "142.250.190.46", DestinationPort: "443", Network: "tcp",
		},
		Rule: "DomainSuffix", RulePayload: "google.com",
		Chains:      []string{"Node-HK-01", "ProxyGroup"},
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

	_ = sink.Emit(&types.CollectorEvent{
		EventID: "ev-api-3", SessionID: "sess-api-test", EpochID: 1, FrameSequence: 3, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(20 * time.Minute), ConnectionID: "c-api-ip-only",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "sync.exe", DestinationIP: "192.0.2.81", DestinationPort: "443", Network: "tcp",
		},
		Rule: "DomainSuffix", RulePayload: "example",
		Chains:      []string{"Node-HK-01", "ProxyGroup"},
		DeltaUpload: 1024, DeltaDownload: 4096,
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
	if sum.ProxyUpload != 2024 || sum.ProxyDownload != 9096 {
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

	// 7.1 /api/v1/analytics/top/rules 契约测试 (包含 (rule, rulePayload, route) + bytes + connectionCount)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/top/rules?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Top rules failed with %d: %s", w.Code, w.Body.String())
	}
	var topRulesRes TopRulesResponse
	if err := json.NewDecoder(w.Body).Decode(&topRulesRes); err != nil {
		t.Fatalf("Failed to decode top rules response: %v", err)
	}
	if len(topRulesRes.Items) == 0 {
		t.Errorf("Expected top rules items, got 0")
	} else {
		firstRule := topRulesRes.Items[0]
		if firstRule.Rule == "" || firstRule.RulePayload == "" || firstRule.Route == "" || firstRule.ConnectionCount <= 0 {
			t.Errorf("Expected TopRuleItem to contain rule, rulePayload, route, connectionCount > 0, got %+v", firstRule)
		}
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
	if len(connDetail.AccountingEvents) == 0 {
		t.Errorf("Expected accountingEvents for c-api-1, got 0")
	} else if connDetail.AccountingEvents[0].Process != "chrome.exe" {
		t.Errorf("Expected event process chrome.exe, got %s", connDetail.AccountingEvents[0].Process)
	}
	if connDetail.AccountingSummary == nil || connDetail.AccountingSummary.LatestProcess != "chrome.exe" {
		t.Errorf("Expected summary LatestProcess chrome.exe, got %+v", connDetail.AccountingSummary)
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

func TestIntelligenceFindingsAPIContractAndRuleFilter(t *testing.T) {
	dbPath, cleanup := createTestDBWithSeedData(t)
	defer cleanup()

	roDB, err := storage.OpenReadOnlyDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnlyDB failed: %v", err)
	}
	defer roDB.Close()

	server, err := NewServer(ServerConfig{DB: roDB, DBPath: dbPath, Token: "intelligence-test-token"})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	mux := http.NewServeMux()
	server.registerRoutes(mux)
	handler := server.AuthAndCORSMiddleware(mux)

	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer intelligence-test-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	if w := call(http.MethodGet, "/api/v1/intelligence/findings"); w.Code != http.StatusBadRequest {
		t.Fatalf("missing range status: got %d body=%s", w.Code, w.Body.String())
	}
	if w := call(http.MethodGet, "/api/v1/intelligence/findings?from=bad&to=2026-09-06T01:00:00Z"); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid timestamp status: got %d", w.Code)
	}
	if w := call(http.MethodGet, "/api/v1/intelligence/findings?from=2026-09-06T02:00:00Z&to=2026-09-06T01:00:00Z"); w.Code != http.StatusBadRequest {
		t.Fatalf("reversed range status: got %d", w.Code)
	}
	if w := call(http.MethodGet, "/api/v1/intelligence/findings?from=2026-09-06T00:00:00Z&to=2026-09-06T01:00:00Z&limitPerKind=0"); w.Code != http.StatusBadRequest {
		t.Fatalf("lower limit status: got %d", w.Code)
	}
	if w := call(http.MethodGet, "/api/v1/intelligence/findings?from=2026-09-06T00:00:00Z&to=2026-09-06T01:00:00Z&limitPerKind=51"); w.Code != http.StatusBadRequest {
		t.Fatalf("upper limit status: got %d", w.Code)
	}
	if w := call(http.MethodPost, "/api/v1/intelligence/findings?from=2026-09-06T00:00:00Z&to=2026-09-06T01:00:00Z"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status: got %d", w.Code)
	}

	valid := call(http.MethodGet, "/api/v1/intelligence/findings?from=2026-09-06T00:00:00Z&to=2026-09-06T03:00:00Z&limitPerKind=1")
	if valid.Code != http.StatusOK {
		t.Fatalf("valid findings status: got %d body=%s", valid.Code, valid.Body.String())
	}
	var result storage.AuditFindingResult
	if err := json.NewDecoder(valid.Body).Decode(&result); err != nil {
		t.Fatalf("decode findings response: %v", err)
	}
	if result.Route != types.RouteProxy || result.LimitPerKind != 1 || result.Items == nil || result.CountsByKind == nil {
		t.Fatalf("unexpected findings response: %+v", result)
	}
	var rawRulePreserved bool
	for _, finding := range result.Items {
		if finding.Kind == storage.AuditFindingIPOnly && finding.Evidence.Rule == "DomainSuffix" {
			if finding.Evidence.RulePayload != "example" {
				t.Fatalf("API finding evidence did not preserve raw DomainSuffix RulePayload: %+v", finding.Evidence)
			}
			rawRulePreserved = true
		}
	}
	if !rawRulePreserved {
		t.Fatalf("API finding evidence did not preserve raw DomainSuffix Rule: %+v", result.Items)
	}

	ruleResponse := call(http.MethodGet, "/api/v1/connections?rule=DomainSuffix&limit=10")
	if ruleResponse.Code != http.StatusOK {
		t.Fatalf("rule filter status: got %d body=%s", ruleResponse.Code, ruleResponse.Body.String())
	}
	var connections ConnectionsListResponse
	if err := json.NewDecoder(ruleResponse.Body).Decode(&connections); err != nil {
		t.Fatalf("decode rule-filter response: %v", err)
	}
	if len(connections.Items) == 0 || connections.Items[0].Rule != "DomainSuffix" {
		t.Fatalf("exact rule filter mismatch: %+v", connections.Items)
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/intelligence/findings?from=2026-09-06T00:00:00Z&to=2026-09-06T01:00:00Z", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status: got %d", unauthenticated.Code)
	}
}

func TestConnectionDetailCompositeIdentityIsolation(t *testing.T) {
	dir, err := os.MkdirTemp("", "proxylens-composite-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "composite.db")
	ctx := context.Background()

	// 1. 在 Session A, Epoch 1 中发射 shared-id 连接 (Chrome)
	sinkA, err := storage.OpenSQLiteSink(ctx, dbPath, "sess-A", "v1.0.0-test")
	if err != nil {
		t.Fatalf("OpenSQLiteSink A failed: %v", err)
	}
	t0 := time.Now().UTC().Add(-1 * time.Hour)
	_ = sinkA.Emit(&types.CollectorEvent{
		EventID: "ev-a-1", SessionID: "sess-A", EpochID: 1, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0, ConnectionID: "shared-conn-123",
		Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "chrome.exe", Host: "google.com", DestinationIP: "142.250.190.46", Network: "tcp",
		},
		Rule: "DomainSuffix", RulePayload: "google.com",
		Chains:      []string{"Node-HK-01", "ProxyGroup"},
		DeltaUpload: 1000, DeltaDownload: 5000,
	})
	_ = sinkA.EndSession(ctx, "sess-A", storage.SessionStatusClosedClean)
	_ = sinkA.Close()

	// 2. 在 Session B, Epoch 2 中发射同一个 shared-id 连接 (Curl)
	sinkB, err := storage.OpenSQLiteSink(ctx, dbPath, "sess-B", "v1.0.0-test")
	if err != nil {
		t.Fatalf("OpenSQLiteSink B failed: %v", err)
	}
	_ = sinkB.Emit(&types.CollectorEvent{
		EventID: "ev-b-1", SessionID: "sess-B", EpochID: 2, FrameSequence: 1, EventSequence: 1,
		Type: types.EventConnectionNew, Timestamp: t0.Add(10 * time.Minute), ConnectionID: "shared-conn-123",
		Route: types.RouteDirect, AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process: "curl.exe", Host: "github.com", DestinationIP: "140.82.112.3", Network: "tcp",
		},
		Rule: "DirectRule", RulePayload: "Direct",
		DeltaUpload: 300, DeltaDownload: 700,
	})
	_ = sinkB.EndSession(ctx, "sess-B", storage.SessionStatusClosedClean)
	_ = sinkB.Close()

	// 3. 执行核算重建
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	if _, err := storage.RebuildAccounting(ctx, db, "composite test run"); err != nil {
		_ = db.Close()
		t.Fatalf("RebuildAccounting failed: %v", err)
	}
	_ = db.Close()

	// 4. 只读 API 查询
	roDB, err := storage.OpenReadOnlyDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnlyDB failed: %v", err)
	}
	defer roDB.Close()

	testToken := "test-composite-token-32-chars"
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

	// 4.1 请求 Session A / Epoch 1 / shared-conn-123
	reqA := httptest.NewRequest(http.MethodGet, "/api/v1/connections/sess-A/1/shared-conn-123", nil)
	reqA.Header.Set("Authorization", "Bearer "+testToken)
	wA := httptest.NewRecorder()
	handler.ServeHTTP(wA, reqA)
	if wA.Code != http.StatusOK {
		t.Fatalf("Request A failed: %d (%s)", wA.Code, wA.Body.String())
	}
	var resA ConnectionDetailResponse
	if err := json.NewDecoder(wA.Body).Decode(&resA); err != nil {
		t.Fatalf("Decode A failed: %v", err)
	}
	if resA.Connection.Metadata.Process != "chrome.exe" {
		t.Errorf("Expected Session A connection process chrome.exe, got %s", resA.Connection.Metadata.Process)
	}
	if len(resA.AccountingEvents) != 1 || resA.AccountingEvents[0].Process != "chrome.exe" {
		t.Errorf("Expected Session A accountingEvents to contain exactly chrome.exe, got %+v", resA.AccountingEvents)
	}
	if resA.AccountingSummary == nil || resA.AccountingSummary.LatestProcess != "chrome.exe" {
		t.Errorf("Expected Session A summary process chrome.exe, got %+v", resA.AccountingSummary)
	}

	// 4.2 请求 Session B / Epoch 2 / shared-conn-123
	reqB := httptest.NewRequest(http.MethodGet, "/api/v1/connections/sess-B/2/shared-conn-123", nil)
	reqB.Header.Set("Authorization", "Bearer "+testToken)
	wB := httptest.NewRecorder()
	handler.ServeHTTP(wB, reqB)
	if wB.Code != http.StatusOK {
		t.Fatalf("Request B failed: %d (%s)", wB.Code, wB.Body.String())
	}
	var resB ConnectionDetailResponse
	if err := json.NewDecoder(wB.Body).Decode(&resB); err != nil {
		t.Fatalf("Decode B failed: %v", err)
	}
	if resB.Connection.Metadata.Process != "curl.exe" {
		t.Errorf("Expected Session B connection process curl.exe, got %s", resB.Connection.Metadata.Process)
	}
	if len(resB.AccountingEvents) != 1 || resB.AccountingEvents[0].Process != "curl.exe" {
		t.Errorf("Expected Session B accountingEvents to contain exactly curl.exe, got %+v", resB.AccountingEvents)
	}
	if resB.AccountingSummary == nil || resB.AccountingSummary.LatestProcess != "curl.exe" {
		t.Errorf("Expected Session B summary process curl.exe, got %+v", resB.AccountingSummary)
	}

	// 4.3 请求不存在的交叉组合 (sess-A, epoch 2, shared-conn-123) -> 必须 404
	reqCross := httptest.NewRequest(http.MethodGet, "/api/v1/connections/sess-A/2/shared-conn-123", nil)
	reqCross.Header.Set("Authorization", "Bearer "+testToken)
	wCross := httptest.NewRecorder()
	handler.ServeHTTP(wCross, reqCross)
	if wCross.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-matching composite identity, got %d", wCross.Code)
	}
}
