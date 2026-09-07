package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func TestConnectionDetailAccountingAuthority(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "authority.db")
	sink, err := storage.OpenSQLiteSink(ctx, path, "detail-session", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	db, err := storage.OpenDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ro, err := storage.OpenReadOnlyDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	server, err := NewServer(ServerConfig{DB: ro, DBPath: path, Token: "detail-test-token"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.registerRoutes(mux)
	handler := server.AuthAndCORSMiddleware(mux)
	read := func(id string) ConnectionDetailResponse {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/connections/detail-session/1/"+id, nil)
		r.Header.Set("Authorization", "Bearer detail-test-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("detail: %d %s", w.Code, w.Body.String())
		}
		var result ConnectionDetailResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	emit := func(frame int64, id string, kind types.EventType, up int64) {
		t.Helper()
		ts := time.Date(2026, 9, 7, 12, 0, int(frame), 0, time.UTC)
		for i, ev := range []*types.CollectorEvent{
			{Type: kind, ConnectionID: id, Route: types.RouteProxy, AttributionClass: types.ClassKnownApplication,
				DeltaUpload: up, DeltaDownload: up * 2, Metadata: types.RawMetadata{Process: "detail.exe", Host: "detail.example", Network: "tcp"}, Rule: "MATCH", Chains: []string{"node", "group"}},
			{Type: types.EventSamplingResidual, Details: map[string]any{"residualUpload": int64(0), "residualDownload": int64(0)}},
		} {
			ev.SessionID, ev.EpochID, ev.FrameSequence, ev.EventSequence, ev.Timestamp = "detail-session", 1, frame, int64(i+1), ts
			ev.GenerateDeterministicEventID()
			if err := sink.Emit(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	emit(1, "existing", types.EventConnectionNew, 100)
	empty := read("existing")
	if len(empty.AccountingEvents) != 0 || empty.AccountingSummary != nil {
		t.Fatal("unaccounted detail must remain available without a summary")
	}
	if _, err := storage.RebuildAccounting(ctx, db, "legacy detail"); err != nil {
		t.Fatal(err)
	}
	legacy := read("existing")
	if len(legacy.AccountingEvents) != 1 || legacy.AccountingSummary == nil || legacy.AccountingSummary.AccountedUploadTotal != 100 {
		t.Fatalf("legacy fallback: %+v", legacy)
	}
	if _, err := storage.AdvanceAccountingV2(ctx, db, "seed detail", 0); err != nil {
		t.Fatal(err)
	}
	gen, err := storage.GetActiveAccountingGeneration(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	v2 := read("existing")
	if v2.AccountingSummary == nil || v2.AccountingSummary.RunID != gen.GenerationID {
		t.Fatal("detail did not select active v2")
	}
	// The public runId field represents either authority key; all other fields agree.
	v2.AccountingSummary.RunID = legacy.AccountingSummary.RunID
	for _, ev := range v2.AccountingEvents {
		ev.RunID = legacy.AccountingSummary.RunID
	}
	if !reflect.DeepEqual(v2, legacy) {
		t.Fatalf("legacy/v2 detail mismatch: legacy=%+v v2=%+v", legacy.AccountingSummary, v2.AccountingSummary)
	}
	emit(2, "recent", types.EventConnectionNew, 700)
	emit(3, "existing", types.EventConnectionDelta, 50)
	if _, err := storage.AdvanceAccountingV2(ctx, db, "advance detail", 0); err != nil {
		t.Fatal(err)
	}
	for id, total := range map[string]int64{"existing": 150, "recent": 700} {
		got := read(id)
		if got.AccountingSummary == nil || got.AccountingSummary.RunID != gen.GenerationID || got.AccountingSummary.AccountedUploadTotal != total || got.AccountingSummary.AccountedDownloadTotal != total*2 {
			t.Fatalf("current v2 %s: %+v", id, got.AccountingSummary)
		}
		for _, ev := range got.AccountingEvents {
			if ev.RunID != gen.GenerationID {
				t.Fatal("mixed authority events")
			}
		}
	}
	// An active generation with no rows for a connection must never fall back per connection.
	if _, err := db.ExecContext(ctx, "DELETE FROM accounted_traffic_v2 WHERE generation_id=? AND connection_id='existing'", gen.GenerationID); err != nil {
		t.Fatal(err)
	}
	if got := read("existing"); len(got.AccountingEvents) != 0 || got.AccountingSummary != nil {
		t.Fatal("stale legacy leaked into active v2 detail")
	}
	if _, err := db.ExecContext(ctx, "UPDATE accounting_generations SET status='superseded' WHERE generation_id=?", gen.GenerationID); err != nil {
		t.Fatal(err)
	}
	if got := read("existing"); !reflect.DeepEqual(got.AccountingSummary, legacy.AccountingSummary) {
		t.Fatal("no-active-generation legacy fallback failed")
	}
}

func TestHistoryTiePagination(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ties.db")
	// Deliberately insert in the opposite order to the composite identity order.
	for _, session := range []string{"b", "a"} {
		sink, err := storage.OpenSQLiteSink(ctx, path, session, "test")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sink.Close() })
		for _, epoch := range []int{1, 2} {
			for i, id := range []string{"z", "a"} {
				ev := &types.CollectorEvent{SessionID: session, EpochID: epoch, FrameSequence: int64(i + 1), EventSequence: 1,
					Type: types.EventConnectionNew, ConnectionID: id, Timestamp: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)}
				ev.GenerateDeterministicEventID()
				if err := sink.Emit(ev); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := sink.Close(); err != nil {
			t.Fatal(err)
		}
	}
	ro, err := storage.OpenReadOnlyDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	q := storage.NewQueryService(ro)
	var got []string
	for offset := 0; offset < 8; offset += 3 {
		rows, err := q.ListConnections(ctx, storage.ConnectionFilter{Limit: 3, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			got = append(got, fmt.Sprintf("%s/%d/%s", row.SessionID, row.EpochID, row.ConnectionID))
		}
	}
	want := []string{"a/1/a", "a/1/z", "a/2/a", "a/2/z", "b/1/a", "b/1/z", "b/2/a", "b/2/z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tie pages: got %v want %v", got, want)
	}
}
