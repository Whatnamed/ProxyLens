package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

var temporalComparisonBaselineFrom = time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
var temporalComparisonBaselineTo = time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC)
var temporalComparisonRecentFrom = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
var temporalComparisonRecentTo = time.Date(2026, 9, 5, 3, 0, 0, 0, time.UTC)

func TestProcessChangesDetectorsAndRates(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "temporal-a-direct", connectionID: "temporal-a-direct", process: "alpha.exe", route: types.RouteDirect, up: 100, down: 400, observedAt: temporalComparisonBaselineFrom.Add(10 * time.Minute)},
		{id: "temporal-a-reject", connectionID: "temporal-a-reject", process: "alpha.exe", route: types.RouteReject, up: 50, down: 50, observedAt: temporalComparisonBaselineFrom.Add(20 * time.Minute)},
		{id: "temporal-a-proxy", connectionID: "temporal-a-proxy", process: "alpha.exe", route: types.RouteProxy, up: 1000, down: 4000, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
		{id: "temporal-b-proxy", connectionID: "temporal-b-proxy", process: "beta.exe", route: types.RouteProxy, up: 3000, down: 4000, observedAt: temporalComparisonRecentFrom.Add(20 * time.Minute)},
		{id: "temporal-c-base", connectionID: "temporal-c-base", process: "growth.exe", route: types.RouteProxy, up: 500, down: 1500, observedAt: temporalComparisonBaselineFrom.Add(30 * time.Minute)},
		{id: "temporal-c-recent-1", connectionID: "temporal-c-recent-1", process: "growth.exe", route: types.RouteProxy, up: 2000, down: 4000, observedAt: temporalComparisonRecentFrom.Add(30 * time.Minute)},
		{id: "temporal-c-recent-2", connectionID: "temporal-c-recent-2", process: "growth.exe", route: types.RouteProxy, up: 1000, down: 1000, observedAt: temporalComparisonRecentFrom.Add(2 * time.Hour), precision: "interval_derived", intervalStart: ptrTime(temporalComparisonRecentFrom.Add(90 * time.Minute)), intervalEnd: ptrTime(temporalComparisonRecentFrom.Add(3 * time.Hour))},
		{id: "temporal-d-base", connectionID: "temporal-d-base", process: "same.exe", route: types.RouteProxy, up: 1000, down: 3000, observedAt: temporalComparisonBaselineFrom.Add(40 * time.Minute)},
		{id: "temporal-d-recent", connectionID: "temporal-d-recent", process: "same.exe", route: types.RouteProxy, up: 1500, down: 4500, observedAt: temporalComparisonRecentFrom.Add(40 * time.Minute)},
		{id: "temporal-e-base", connectionID: "temporal-e-base", process: "lower.exe", route: types.RouteProxy, up: 2000, down: 4000, observedAt: temporalComparisonBaselineFrom.Add(50 * time.Minute)},
		{id: "temporal-e-recent", connectionID: "temporal-e-recent", process: "lower.exe", route: types.RouteProxy, up: 300, down: 700, observedAt: temporalComparisonRecentFrom.Add(50 * time.Minute)},
		{id: "temporal-f-direct", connectionID: "temporal-f-direct", process: "direct-only.exe", route: types.RouteDirect, up: 100, down: 200, observedAt: temporalComparisonRecentFrom.Add(time.Hour)},
	}

	for _, useV2 := range []bool{false, true} {
		name := "legacy"
		if useV2 {
			name = "v2"
		}
		t.Run(name, func(t *testing.T) {
			db, cleanup := buildTemporalFixture(t, events, useV2)
			defer cleanup()

			result := listTemporalChanges(t, db, validTemporalFilter())
			if result.Status != ComparisonReady {
				t.Fatalf("status=%s result=%+v", result.Status, result)
			}
			newItems := temporalItemsOfKind(result.Items, TemporalProcessNewlyObservedOnProxy)
			if len(newItems) != 2 || newItems[0].Process != "beta.exe" || newItems[1].Process != "alpha.exe" {
				t.Fatalf("newly observed ordering/items=%+v", newItems)
			}
			if newItems[1].Baseline.Direct.TotalBytes != 500 || newItems[1].Baseline.Reject.TotalBytes != 100 {
				t.Fatalf("baseline route context was not preserved: %+v", newItems[1].Baseline)
			}
			growthItems := temporalItemsOfKind(result.Items, TemporalProcessProxyGrowth)
			if len(growthItems) != 1 || growthItems[0].Process != "growth.exe" {
				t.Fatalf("growth filtering/items=%+v", growthItems)
			}
			growth := growthItems[0]
			if growth.BaselineProxyBytesPerHour != 1000 {
				t.Fatalf("baseline rate=%v, want 1000", growth.BaselineProxyBytesPerHour)
			}
			if growth.GrowthRatio == nil || math.Abs(*growth.GrowthRatio-(5.0/3.0)) > 1e-9 {
				t.Fatalf("growth ratio=%v, want 1.666666...", growth.GrowthRatio)
			}
			if growth.RecentProxyBytesPerHour <= growth.BaselineProxyBytesPerHour || growth.DeltaBytesPerHour <= 0 {
				t.Fatalf("growth rate evidence=%+v", growth)
			}
			if growth.Recent.Proxy.EstimatedUploadBytes == 0 {
				t.Fatalf("interval-derived recent evidence was not preserved: %+v", growth.Recent.Proxy)
			}
			if len(temporalItemsOfKind(result.Items, TemporalProcessProxyGrowth)) != result.CountsByKind[TemporalProcessProxyGrowth] {
				t.Fatalf("counts must report all matching growth items: %+v", result.CountsByKind)
			}
		})
	}
}

func TestProcessChangesLegacyV2Equivalence(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "equiv-new-direct", connectionID: "equiv-new-direct", process: "same-new.exe", route: types.RouteDirect, up: 1, down: 2, observedAt: temporalComparisonBaselineFrom.Add(10 * time.Minute)},
		{id: "equiv-new-proxy", connectionID: "equiv-new-proxy", process: "same-new.exe", route: types.RouteProxy, up: 10, down: 90, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
		{id: "equiv-growth-base", connectionID: "equiv-growth-base", process: "same-growth.exe", route: types.RouteProxy, up: 100, down: 900, observedAt: temporalComparisonBaselineFrom.Add(20 * time.Minute)},
		{id: "equiv-growth-recent", connectionID: "equiv-growth-recent", process: "same-growth.exe", route: types.RouteProxy, up: 400, down: 3600, observedAt: temporalComparisonRecentFrom.Add(20 * time.Minute)},
	}
	legacyDB, legacyCleanup := buildTemporalFixture(t, events, false)
	defer legacyCleanup()
	v2DB, v2Cleanup := buildTemporalFixture(t, events, true)
	defer v2Cleanup()

	legacy := listTemporalChanges(t, legacyDB, validTemporalFilter())
	v2 := listTemporalChanges(t, v2DB, validTemporalFilter())
	legacy.AccountingVersion = ""
	v2.AccountingVersion = ""
	if !reflect.DeepEqual(legacy, v2) {
		t.Fatalf("legacy/v2 temporal results differ:\nlegacy=%+v\nv2=%+v", legacy, v2)
	}
}

func TestHostRouteChangeDetectorSemanticsAndStableIDs(t *testing.T) {
	intervalStart := temporalComparisonRecentFrom.Add(time.Hour)
	intervalEnd := intervalStart.Add(time.Hour)
	longHost := "very-long-recorded-host-name-for-route-transition-review.example"
	events := []intelligenceFixtureEvent{
		{id: "host-direct-base", connectionID: "host-direct-base", host: "host-direct-to-proxy.example", route: types.RouteDirect, up: 100, down: 900, observedAt: temporalComparisonBaselineFrom.Add(10 * time.Minute)},
		{id: "host-direct-recent", connectionID: "host-direct-recent", host: "host-direct-to-proxy.example", route: types.RouteProxy, up: 200, down: 1800, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
		{id: "host-mixed-base", connectionID: "host-mixed-base", host: "host-mixed-recent.example", route: types.RouteDirect, up: 50, down: 450, observedAt: temporalComparisonBaselineFrom.Add(20 * time.Minute)},
		{id: "host-mixed-proxy", connectionID: "host-mixed-proxy", host: "host-mixed-recent.example", route: types.RouteProxy, up: 300, down: 2700, observedAt: temporalComparisonRecentFrom.Add(20 * time.Minute)},
		{id: "host-mixed-direct", connectionID: "host-mixed-direct", host: "host-mixed-recent.example", route: types.RouteDirect, up: 25, down: 225, observedAt: temporalComparisonRecentFrom.Add(30 * time.Minute)},
		{id: "host-always-proxy-base", connectionID: "host-always-proxy-base", host: "host-always-proxy.example", route: types.RouteProxy, up: 100, down: 900, observedAt: temporalComparisonBaselineFrom.Add(40 * time.Minute)},
		{id: "host-always-proxy-recent", connectionID: "host-always-proxy-recent", host: "host-always-proxy.example", route: types.RouteProxy, up: 200, down: 1800, observedAt: temporalComparisonRecentFrom.Add(40 * time.Minute)},
		{id: "host-stays-direct-base", connectionID: "host-stays-direct-base", host: "host-stays-direct.example", route: types.RouteDirect, up: 100, down: 900, observedAt: temporalComparisonBaselineFrom.Add(50 * time.Minute)},
		{id: "host-stays-direct-recent", connectionID: "host-stays-direct-recent", host: "host-stays-direct.example", route: types.RouteDirect, up: 200, down: 1800, observedAt: temporalComparisonRecentFrom.Add(50 * time.Minute)},
		{id: "host-new-only-recent", connectionID: "host-new-only-recent", host: "host-new-only.example", route: types.RouteProxy, up: 200, down: 1800, observedAt: temporalComparisonRecentFrom.Add(time.Hour)},
		{id: "host-reject-base", connectionID: "host-reject-base", host: "host-reject-baseline.example", route: types.RouteReject, up: 100, down: 900, observedAt: temporalComparisonBaselineFrom.Add(70 * time.Minute)},
		{id: "host-reject-recent", connectionID: "host-reject-recent", host: "host-reject-baseline.example", route: types.RouteProxy, up: 200, down: 1800, observedAt: temporalComparisonRecentFrom.Add(70 * time.Minute)},
		{id: "host-empty-recent", connectionID: "host-empty-recent", host: "", route: types.RouteProxy, up: 200, down: 1800, observedAt: temporalComparisonRecentFrom.Add(80 * time.Minute)},
		{id: "host-long-base", connectionID: "host-long-base", host: longHost, route: types.RouteDirect, up: 100, down: 900, observedAt: temporalComparisonBaselineFrom.Add(80 * time.Minute)},
		{id: "host-long-recent", connectionID: "host-long-recent", host: longHost, route: types.RouteProxy, up: 400, down: 3600, observedAt: temporalComparisonRecentFrom.Add(80 * time.Minute)},
		{id: "host-interval-base", connectionID: "host-interval-base", host: "host-interval.example", route: types.RouteDirect, up: 100, down: 900, observedAt: temporalComparisonBaselineFrom.Add(90 * time.Minute)},
		{id: "host-interval-recent", connectionID: "host-interval-recent", host: "host-interval.example", route: types.RouteProxy, up: 1000, down: 3000, observedAt: intervalStart, precision: "interval_derived", intervalStart: &intervalStart, intervalEnd: &intervalEnd},
	}

	for _, useV2 := range []bool{false, true} {
		name := "legacy"
		if useV2 {
			name = "v2"
		}
		t.Run(name, func(t *testing.T) {
			db, cleanup := buildTemporalFixture(t, events, useV2)
			defer cleanup()

			result, err := NewAuditIntelligenceService(db).ListTemporalFindings(context.Background(), validTemporalFilter())
			if err != nil {
				t.Fatalf("ListTemporalFindings failed: %v", err)
			}
			if result.Status != ComparisonReady {
				t.Fatalf("status=%s", result.Status)
			}
			if got := result.CountsByKind[TemporalHostGainedProxyAfterDirect]; got != 4 {
				t.Fatalf("host finding count=%d, want 4; items=%+v", got, result.HostItems)
			}
			byHost := make(map[string]HostRouteChangeFinding, len(result.HostItems))
			for _, item := range result.HostItems {
				byHost[item.Host] = item
			}
			for _, expected := range []string{"host-direct-to-proxy.example", "host-mixed-recent.example", longHost, "host-interval.example"} {
				if _, ok := byHost[expected]; !ok {
					t.Fatalf("expected host finding %q in %+v", expected, result.HostItems)
				}
			}
			for _, excluded := range []string{"host-always-proxy.example", "host-stays-direct.example", "host-new-only.example", "host-reject-baseline.example", ""} {
				if _, ok := byHost[excluded]; ok {
					t.Fatalf("host %q must not be a route transition finding", excluded)
				}
			}
			mixed := byHost["host-mixed-recent.example"]
			if mixed.Baseline.Direct.TotalBytes != 500 || mixed.Baseline.Proxy.TotalBytes != 0 || mixed.Recent.Proxy.TotalBytes != 3000 || mixed.Recent.Direct.TotalBytes != 250 {
				t.Fatalf("mixed route evidence was not preserved: %+v", mixed)
			}
			interval := byHost["host-interval.example"]
			if interval.Recent.Proxy.EstimatedUploadBytes == 0 || interval.Recent.Proxy.EstimatedDownloadBytes == 0 || interval.Recent.Proxy.ExactUploadBytes != 0 {
				t.Fatalf("interval-derived host evidence was not preserved: %+v", interval.Recent.Proxy)
			}

			wider := validTemporalFilter()
			wider.RecentTo = ptrTime(temporalComparisonRecentTo.Add(time.Hour))
			later, err := NewAuditIntelligenceService(db).ListTemporalFindings(context.Background(), wider)
			if err != nil {
				t.Fatalf("wider ListTemporalFindings failed: %v", err)
			}
			laterIDs := make(map[string]string, len(later.HostItems))
			for _, item := range later.HostItems {
				laterIDs[item.Host] = item.ID
			}
			for _, item := range result.HostItems {
				if laterIDs[item.Host] != item.ID {
					t.Fatalf("host ID changed across window/evidence boundary for %q: before=%q after=%q", item.Host, item.ID, laterIDs[item.Host])
				}
			}
		})
	}
}

func TestTemporalFindingsLegacyV2HostAndProcessEquivalence(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "generic-direct", connectionID: "generic-direct", process: "generic.exe", host: "generic.example", route: types.RouteDirect, up: 10, down: 20, observedAt: temporalComparisonBaselineFrom.Add(10 * time.Minute)},
		{id: "generic-proxy", connectionID: "generic-proxy", process: "generic.exe", host: "generic.example", route: types.RouteProxy, up: 100, down: 200, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
		{id: "generic-growth-base", connectionID: "generic-growth-base", process: "growth-generic.exe", host: "growth-generic.example", route: types.RouteProxy, up: 10, down: 20, observedAt: temporalComparisonBaselineFrom.Add(20 * time.Minute)},
		{id: "generic-growth-recent", connectionID: "generic-growth-recent", process: "growth-generic.exe", host: "growth-generic.example", route: types.RouteProxy, up: 100, down: 200, observedAt: temporalComparisonRecentFrom.Add(20 * time.Minute)},
	}
	legacyDB, legacyCleanup := buildTemporalFixture(t, events, false)
	defer legacyCleanup()
	v2DB, v2Cleanup := buildTemporalFixture(t, events, true)
	defer v2Cleanup()

	legacy, err := NewAuditIntelligenceService(legacyDB).ListTemporalFindings(context.Background(), validTemporalFilter())
	if err != nil {
		t.Fatalf("legacy temporal findings failed: %v", err)
	}
	v2, err := NewAuditIntelligenceService(v2DB).ListTemporalFindings(context.Background(), validTemporalFilter())
	if err != nil {
		t.Fatalf("v2 temporal findings failed: %v", err)
	}
	legacy.AccountingVersion = ""
	v2.AccountingVersion = ""
	if !reflect.DeepEqual(legacy, v2) {
		t.Fatalf("legacy/v2 generic temporal results differ:\nlegacy=%+v\nv2=%+v", legacy, v2)
	}
}

func TestProcessChangesCoverageIsFailClosed(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "coverage-baseline", connectionID: "coverage-baseline", process: "covered.exe", route: types.RouteProxy, up: 10, down: 20, observedAt: temporalComparisonBaselineFrom.Add(10 * time.Minute)},
		{id: "coverage-recent", connectionID: "coverage-recent", process: "covered.exe", route: types.RouteProxy, up: 30, down: 40, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
	}
	db, cleanup := buildTemporalFixture(t, events, false)
	defer cleanup()

	_, err := db.ExecContext(context.Background(), `
		INSERT INTO monitoring_gaps (
			gap_id, source, session_id, started_at, ended_at, duration_ms, reason,
			global_gap_upload_delta, global_gap_download_delta, physical_delta_unavailable, precision, created_at
		) VALUES (?, 'controller_stream', 'sess-temporal', ?, ?, ?, 'synthetic baseline gap', 0, 0, 1, 'interval_derived', ?);
	`, "temporal-gap", temporalComparisonBaselineFrom.Add(30*time.Minute).Format(time.RFC3339Nano), temporalComparisonBaselineFrom.Add(time.Hour).Format(time.RFC3339Nano), int64(30*time.Minute/time.Millisecond), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("insert coverage gap: %v", err)
	}
	result := listTemporalChanges(t, db, validTemporalFilter())
	if result.Status != ComparisonBaselineHasMonitoringGaps || len(result.Items) != 0 {
		t.Fatalf("coverage gap must suppress findings: status=%s items=%+v", result.Status, result.Items)
	}

	if got := comparisonCoverageStatus("baseline", &CoverageSummary{FutureDurationMs: 1}); got != ComparisonBaselineFuture {
		t.Fatalf("future status=%s", got)
	}
	if got := comparisonCoverageStatus("recent", &CoverageSummary{OutsideKnownScopeMs: 1}); got != ComparisonRecentOutsideKnownScope {
		t.Fatalf("outside status=%s", got)
	}
}

func TestProcessChangesValidatesExplicitFullHourWindows(t *testing.T) {
	base := validTemporalFilter()
	cases := []ProcessChangeFilter{
		{BaselineFrom: base.BaselineFrom, BaselineTo: base.BaselineTo, RecentFrom: base.RecentFrom, RecentTo: ptrTime(temporalComparisonRecentFrom.Add(30 * time.Minute))},
		{BaselineFrom: ptrTime(temporalComparisonBaselineFrom.Add(30 * time.Minute)), BaselineTo: base.BaselineTo, RecentFrom: base.RecentFrom, RecentTo: base.RecentTo},
		{BaselineFrom: base.BaselineFrom, BaselineTo: ptrTime(temporalComparisonRecentFrom.Add(time.Hour)), RecentFrom: base.RecentFrom, RecentTo: base.RecentTo},
	}
	for index, filter := range cases {
		if err := validateProcessChangeFilter(filter); err == nil {
			t.Fatalf("case %d should be rejected", index)
		}
	}
	if err := validateProcessChangeFilter(ProcessChangeFilter{
		BaselineFrom: ptrTime(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)),
		BaselineTo:   ptrTime(time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)),
		RecentFrom:   ptrTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		RecentTo:     ptrTime(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)),
	}); err == nil {
		t.Fatal("baseline chronologically after recent should be rejected")
	}
}

func TestProcessChangesGrowthRatioContract(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "ratio-baseline", connectionID: "ratio-baseline", process: "ratio.exe", route: types.RouteProxy, up: 500, down: 1500, observedAt: temporalComparisonBaselineFrom.Add(10 * time.Minute)},
		{id: "ratio-recent", connectionID: "ratio-recent", process: "ratio.exe", route: types.RouteProxy, up: 1500, down: 3000, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
	}
	db, cleanup := buildTemporalFixture(t, events, false)
	defer cleanup()

	result := listTemporalChanges(t, db, validTemporalFilter())
	growth := temporalItemsOfKind(result.Items, TemporalProcessProxyGrowth)
	if len(growth) != 1 || growth[0].GrowthRatio == nil {
		t.Fatalf("expected one growth ratio finding, got %+v", result)
	}
	if *growth[0].GrowthRatio != 0.5 {
		t.Fatalf("growthRatio=%v, want 0.5 (+50%%)", *growth[0].GrowthRatio)
	}
}

func TestProcessChangesAccountingWindowCompleteness(t *testing.T) {
	for _, useV2 := range []bool{false, true} {
		name := "legacy"
		if useV2 {
			name = "v2"
		}
		t.Run(name, func(t *testing.T) {
			baseEvents := []intelligenceFixtureEvent{
				{id: "completeness-base", connectionID: "completeness-base", process: "covered.exe", route: types.RouteProxy, up: 10, down: 20, observedAt: temporalComparisonBaselineFrom.Add(10 * time.Minute)},
				{id: "completeness-recent", connectionID: "completeness-recent", process: "covered.exe", route: types.RouteProxy, up: 30, down: 40, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
			}

			t.Run("baseline-incomplete", func(t *testing.T) {
				db, cleanup := buildTemporalFixture(t, baseEvents, useV2)
				defer cleanup()
				insertUnpublishedTemporalJournalEvent(t, db, temporalComparisonBaselineFrom.Add(30*time.Minute), "baseline")
				result := listTemporalChanges(t, db, validTemporalFilter())
				if result.Status != ComparisonBaselineAccountingIncomplete || len(result.Items) != 0 {
					t.Fatalf("baseline unpublished evidence must suppress findings: status=%s items=%+v", result.Status, result.Items)
				}
			})

			t.Run("recent-incomplete", func(t *testing.T) {
				db, cleanup := buildTemporalFixture(t, baseEvents, useV2)
				defer cleanup()
				insertUnpublishedTemporalJournalEvent(t, db, temporalComparisonRecentFrom.Add(30*time.Minute), "recent")
				result := listTemporalChanges(t, db, validTemporalFilter())
				if result.Status != ComparisonRecentAccountingIncomplete || len(result.Items) != 0 {
					t.Fatalf("recent unpublished evidence must suppress findings: status=%s items=%+v", result.Status, result.Items)
				}
			})

			t.Run("lag-after-window-does-not-block", func(t *testing.T) {
				db, cleanup := buildTemporalFixture(t, baseEvents, useV2)
				defer cleanup()
				before := listTemporalChanges(t, db, validTemporalFilter())
				insertUnpublishedTemporalJournalEvent(t, db, temporalComparisonRecentTo.Add(10*time.Minute), "after-window")
				after := listTemporalChanges(t, db, validTemporalFilter())
				if after.Status != ComparisonReady || !reflect.DeepEqual(before.Items, after.Items) {
					t.Fatalf("post-window journal lag must not change comparison: before=%+v after=%+v", before, after)
				}
			})

			t.Run("published-boundary-preserves-detectors", func(t *testing.T) {
				db, cleanup := buildTemporalFixture(t, baseEvents, useV2)
				defer cleanup()
				result := listTemporalChanges(t, db, validTemporalFilter())
				if result.Status != ComparisonReady || len(result.Items) != 1 || result.Items[0].Process != "covered.exe" {
					t.Fatalf("published boundary must preserve detector output: %+v", result)
				}
			})
		})
	}
}

func TestProcessChangesLegacyWithoutAccountingBoundaryFailsClosed(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "boundary-unavailable", connectionID: "boundary-unavailable", process: "covered.exe", route: types.RouteProxy, up: 10, down: 20, observedAt: temporalComparisonRecentFrom.Add(10 * time.Minute)},
	}
	db, cleanup := buildTemporalFixture(t, events, false)
	defer cleanup()
	if _, err := db.ExecContext(context.Background(), `UPDATE accounting_runs SET source_journal_sequence_max = NULL WHERE run_id = (SELECT run_id FROM accounting_runs WHERE status = 'completed' ORDER BY started_at DESC LIMIT 1);`); err != nil {
		t.Fatalf("clear legacy authority boundary: %v", err)
	}
	result := listTemporalChanges(t, db, validTemporalFilter())
	if result.Status != ComparisonAccountingBoundaryUnavailable || len(result.Items) != 0 {
		t.Fatalf("missing legacy boundary must fail closed: status=%s items=%+v", result.Status, result.Items)
	}
}

func TestProcessChangesHighCardinalityTimingsAndQueryPlans(t *testing.T) {
	for _, scale := range []int{10_000, 100_000} {
		for _, useV2 := range []bool{false, true} {
			name := fmt.Sprintf("rows_%d_%s", scale, map[bool]string{false: "legacy", true: "v2"}[useV2])
			t.Run(name, func(t *testing.T) {
				db := buildScaledTemporalHourlyDB(t, scale, useV2)
				defer db.Close()

				plan := explainTemporalHourlyQuery(t, db, useV2)
				started := time.Now()
				result := listTemporalChanges(t, db, validTemporalFilter())
				queryDuration := time.Since(started)
				if result.Status != ComparisonReady || len(result.Items) == 0 {
					t.Fatalf("scaled temporal query did not produce ready findings: %+v", result)
				}
				t.Logf("temporal-scale rows=%d high-cardinality=true authority=%s baseline-read+recent-read+detectors=%s total=%s eqp=%s", scale, map[bool]string{false: "legacy", true: "v2"}[useV2], queryDuration, queryDuration, strings.Join(plan, " | "))
			})
		}
	}
}

func TestTemporalFindingsHighCardinalityTimingsAndQueryPlans(t *testing.T) {
	for _, scale := range []int{10_000, 100_000} {
		for _, useV2 := range []bool{false, true} {
			name := fmt.Sprintf("rows_%d_%s", scale, map[bool]string{false: "legacy", true: "v2"}[useV2])
			t.Run(name, func(t *testing.T) {
				fixtureStarted := time.Now()
				db := buildScaledTemporalHourlyDB(t, scale, useV2)
				fixtureDuration := time.Since(fixtureStarted)
				defer db.Close()

				plan := explainTemporalCombinedHourlyQuery(t, db, useV2)
				service := NewAuditIntelligenceService(db)
				analytics := NewAnalyticsService(db)
				scope, err := analytics.resolveAccountingScope(context.Background())
				if err != nil {
					t.Fatalf("resolve scaled temporal scope: %v", err)
				}
				table, keyColumn, keyValue := scope.temporalWindowQuery()
				baselineReadStarted := time.Now()
				baseline, err := service.loadHourlyDimensionTotals(context.Background(), table, keyColumn, keyValue, []string{"process", "host"}, temporalComparisonBaselineFrom, temporalComparisonBaselineTo)
				baselineReadDuration := time.Since(baselineReadStarted)
				if err != nil {
					t.Fatalf("scaled baseline combined read: %v", err)
				}
				recentReadStarted := time.Now()
				recent, err := service.loadHourlyDimensionTotals(context.Background(), table, keyColumn, keyValue, []string{"process", "host"}, temporalComparisonRecentFrom, temporalComparisonRecentTo)
				recentReadDuration := time.Since(recentReadStarted)
				if err != nil {
					t.Fatalf("scaled recent combined read: %v", err)
				}
				processDetectorStarted := time.Now()
				processItems, growthItems := buildProcessFindings(baseline["process"], recent["process"], 2, 3)
				processDetectorDuration := time.Since(processDetectorStarted)
				hostDetectorStarted := time.Now()
				hostItems := buildHostRouteChangeFindings(baseline["host"], recent["host"])
				hostDetectorDuration := time.Since(hostDetectorStarted)

				serviceStarted := time.Now()
				result, err := service.ListTemporalFindings(context.Background(), validTemporalFilter())
				totalServiceDuration := time.Since(serviceStarted)
				if err != nil {
					t.Fatalf("scaled ListTemporalFindings: %v", err)
				}
				if result.Status != ComparisonReady || len(result.ProcessItems) == 0 || len(processItems) == 0 {
					t.Fatalf("scaled generic temporal query did not produce process findings: %+v", result)
				}
				t.Logf("temporal-combined-scale rows=%d authority=%s fixture=%s baselineCombinedRead=%s recentCombinedRead=%s processDetector=%s hostDetector=%s totalService=%s processFindings=%d/%d hostItems=%d eqp=%s", scale, map[bool]string{false: "legacy", true: "v2"}[useV2], fixtureDuration, baselineReadDuration, recentReadDuration, processDetectorDuration, hostDetectorDuration, totalServiceDuration, len(processItems), len(growthItems), len(hostItems), strings.Join(plan, " | "))
			})
		}
	}
}

func validTemporalFilter() ProcessChangeFilter {
	return ProcessChangeFilter{
		BaselineFrom: &temporalComparisonBaselineFrom,
		BaselineTo:   &temporalComparisonBaselineTo,
		RecentFrom:   &temporalComparisonRecentFrom,
		RecentTo:     &temporalComparisonRecentTo,
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

func insertUnpublishedTemporalJournalEvent(t *testing.T, db *sql.DB, observedAt time.Time, label string) {
	t.Helper()
	var journalSequence int64
	if err := db.QueryRowContext(context.Background(), `SELECT COALESCE(MAX(journal_sequence), 0) + 1 FROM event_journal;`).Scan(&journalSequence); err != nil {
		t.Fatalf("next journal sequence for unpublished %s evidence: %v", label, err)
	}
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO event_journal (
			event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type,
			observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
		) VALUES (?, 'sess-intelligence', 1, ?, 1, 'ConnectionDelta', ?, ?, '{}', ?, ?, ?);
	`, "unpublished-"+label+fmt.Sprintf("-%d", journalSequence), 10000+journalSequence, observedAt.Format(time.RFC3339Nano), "unpublished-"+label, fmt.Sprintf("%064x", journalSequence), time.Now().UTC().Format(time.RFC3339Nano), journalSequence)
	if err != nil {
		t.Fatalf("insert unpublished %s evidence: %v", label, err)
	}
}

func listTemporalChanges(t *testing.T, db *sql.DB, filter ProcessChangeFilter) *ProcessChangeResult {
	t.Helper()
	result, err := NewAuditIntelligenceService(db).ListProcessChanges(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListProcessChanges failed: %v", err)
	}
	return result
}

func temporalItemsOfKind(items []ProcessChangeFinding, kind TemporalFindingKind) []ProcessChangeFinding {
	result := make([]ProcessChangeFinding, 0)
	for _, item := range items {
		if item.Kind == kind {
			result = append(result, item)
		}
	}
	return result
}

func buildTemporalFixture(t *testing.T, events []intelligenceFixtureEvent, useV2 bool) (*sql.DB, func()) {
	t.Helper()
	db, cleanup := buildIntelligenceFixture(t, events, useV2)
	_, err := db.ExecContext(context.Background(), `
		UPDATE collector_sessions
		SET started_at = ?, ended_at = ?, last_event_at = ?, last_heartbeat_at = ?, updated_at = ?
		WHERE session_id = 'sess-intelligence';
	`, temporalComparisonBaselineFrom.Add(-time.Hour).Format(time.RFC3339Nano), temporalComparisonRecentTo.Add(time.Hour).Format(time.RFC3339Nano), temporalComparisonRecentTo.Add(-time.Minute).Format(time.RFC3339Nano), temporalComparisonRecentTo.Format(time.RFC3339Nano), temporalComparisonRecentTo.Format(time.RFC3339Nano))
	if err != nil {
		cleanup()
		t.Fatalf("normalize temporal session: %v", err)
	}
	return db, cleanup
}

func buildScaledTemporalHourlyDB(t *testing.T, rows int, useV2 bool) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "temporal-scale.db")
	db, err := OpenDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenDB scaled temporal: %v", err)
	}
	ctx := context.Background()
	started := temporalComparisonBaselineFrom.Add(-time.Hour).Format(time.RFC3339Nano)
	ended := temporalComparisonRecentTo.Add(time.Hour).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO collector_sessions (
			session_id, started_at, ended_at, last_event_at, last_frame_sequence, status,
			collector_version, created_at, updated_at, last_heartbeat_at, heartbeat_interval_ms
		) VALUES ('sess-scale-temporal', ?, ?, ?, 1, 'closed_clean', 'test', ?, ?, ?, 5000);
	`, started, ended, ended, started, ended, ended); err != nil {
		db.Close()
		t.Fatalf("insert scaled session: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		t.Fatalf("begin scaled temporal tx: %v", err)
	}
	defer tx.Rollback()
	if useV2 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO accounting_generations (
				generation_id, algorithm_version, derivation_version, status,
				published_journal_sequence, notes, created_at, activated_at
			) VALUES ('gen-scale-temporal', 'incremental-accounting-v2', 'test', 'active', 0, 'temporal scale', ?, ?);
		`, started, started); err != nil {
			db.Close()
			t.Fatalf("insert scaled v2 generation: %v", err)
		}
	} else if _, err := tx.ExecContext(ctx, `
		INSERT INTO accounting_runs (
			run_id, algorithm_version, started_at, completed_at, status,
			source_journal_event_count, source_journal_sequence_max, source_boundary_json, notes
		) VALUES ('run-scale-temporal', 'legacy-v1', ?, ?, 'completed', ?, 0, '{}', 'temporal scale');
	`, started, started, rows); err != nil {
		db.Close()
		t.Fatalf("insert scaled legacy run: %v", err)
	}

	table := "usage_hourly_dimensions"
	key := "run-scale-temporal"
	if useV2 {
		table = "usage_hourly_dimensions_v2"
		key = "gen-scale-temporal"
	}
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			%s, bucket_start, dimension_type, dimension_key, route,
			upload_bytes, download_bytes, connection_count,
			exact_upload_bytes, exact_download_bytes, estimated_upload_bytes, estimated_download_bytes
		) VALUES (?, ?, ?, ?, 'PROXY', ?, ?, 1, ?, ?, 0, 0);
	`, table, map[bool]string{false: "run_id", true: "generation_id"}[useV2]))
	if err != nil {
		db.Close()
		t.Fatalf("prepare scaled hourly insert: %v", err)
	}
	defer stmt.Close()
	for index := 0; index < rows; index++ {
		bucket := temporalComparisonBaselineFrom.Add(time.Duration(index%2) * time.Hour)
		if index%2 == 1 {
			bucket = temporalComparisonRecentFrom.Add(time.Duration(index%3) * time.Hour)
		}
		bytes := int64(1024 + index%4096)
		if _, err := stmt.ExecContext(ctx, key, bucket.Format(time.RFC3339Nano), "process", fmt.Sprintf("scale-process-%06d.exe", index), bytes, bytes*2, bytes, bytes*2); err != nil {
			db.Close()
			t.Fatalf("insert scaled hourly row %d: %v", index, err)
		}
		if index%2 == 1 {
			if _, err := stmt.ExecContext(ctx, key, bucket.Format(time.RFC3339Nano), "host", fmt.Sprintf("scale-host-%06d.example", index), bytes, bytes*2, bytes, bytes*2); err != nil {
				db.Close()
				t.Fatalf("insert scaled hourly host row %d: %v", index, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		db.Close()
		t.Fatalf("commit scaled temporal rows: %v", err)
	}
	return db
}

func explainTemporalHourlyQuery(t *testing.T, db *sql.DB, useV2 bool) []string {
	t.Helper()
	table := "usage_hourly_dimensions"
	keyColumn := "run_id"
	keyValue := "run-scale-temporal"
	if useV2 {
		table = "usage_hourly_dimensions_v2"
		keyColumn = "generation_id"
		keyValue = "gen-scale-temporal"
	}
	rows, err := db.QueryContext(context.Background(), fmt.Sprintf(`
		EXPLAIN QUERY PLAN
		SELECT dimension_key, route, SUM(upload_bytes), SUM(download_bytes)
		FROM %s
		WHERE %s = ? AND dimension_type = 'process' AND bucket_start >= ? AND bucket_start < ?
		GROUP BY dimension_key, route;
	`, table, keyColumn), keyValue, temporalComparisonBaselineFrom.Format(time.RFC3339Nano), temporalComparisonBaselineTo.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("explain temporal query: %v", err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id int
		var parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan temporal query plan: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("temporal query plan rows: %v", err)
	}
	sort.Strings(details)
	return details
}

func explainTemporalCombinedHourlyQuery(t *testing.T, db *sql.DB, useV2 bool) []string {
	t.Helper()
	table := "usage_hourly_dimensions"
	keyColumn := "run_id"
	keyValue := "run-scale-temporal"
	if useV2 {
		table = "usage_hourly_dimensions_v2"
		keyColumn = "generation_id"
		keyValue = "gen-scale-temporal"
	}
	rows, err := db.QueryContext(context.Background(), fmt.Sprintf(`
		EXPLAIN QUERY PLAN
		SELECT dimension_type, dimension_key, route, SUM(upload_bytes), SUM(download_bytes)
		FROM %s
		WHERE %s = ? AND dimension_type IN ('process', 'host') AND bucket_start >= ? AND bucket_start < ?
		GROUP BY dimension_type, dimension_key, route;
	`, table, keyColumn), keyValue, temporalComparisonBaselineFrom.Format(time.RFC3339Nano), temporalComparisonBaselineTo.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("explain combined temporal query: %v", err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan combined temporal query plan: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("combined temporal query plan rows: %v", err)
	}
	sort.Strings(details)
	return details
}
