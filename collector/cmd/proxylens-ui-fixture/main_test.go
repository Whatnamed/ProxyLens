package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

func TestReviewRouteShiftFixtureHostSetIsAnchorMinuteIndependent(t *testing.T) {
	anchors := []time.Time{
		time.Date(2026, 9, 6, 13, 5, 0, 0, time.UTC),
		time.Date(2026, 9, 6, 13, 55, 0, 0, time.UTC),
	}

	var expectedHosts []string
	for index, anchor := range anchors {
		dbPath := filepath.Join(t.TempDir(), "fixture_review-route-shift.db")
		generateReviewRouteShift(context.Background(), dbPath, anchor)

		db, err := storage.OpenReadOnlyDB(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("anchor %s: OpenReadOnlyDB failed: %v", anchor.Format(time.RFC3339), err)
		}

		result, err := storage.NewAuditIntelligenceService(db).ListTemporalFindings(context.Background(), storage.TemporalFindingsFilter{
			BaselineFrom: routeShiftBaselineBucket(anchor),
			BaselineTo:   timePtr(routeShiftBaselineBucket(anchor).Add(time.Hour)),
			RecentFrom:   timePtr(anchor.UTC().Truncate(time.Hour).Add(-time.Hour)),
			RecentTo:     timePtr(anchor.UTC().Truncate(time.Hour)),
			LimitPerKind: 20,
		})
		closeErr := db.Close()
		if err != nil {
			t.Fatalf("anchor %s: ListTemporalFindings failed: %v", anchor.Format(time.RFC3339), err)
		}
		if closeErr != nil {
			t.Fatalf("anchor %s: close read-only DB: %v", anchor.Format(time.RFC3339), closeErr)
		}
		if result.Status != storage.ComparisonReady {
			t.Fatalf("anchor %s: temporal status=%s, want ready", anchor.Format(time.RFC3339), result.Status)
		}

		hosts := make([]string, 0, len(result.HostItems))
		for _, finding := range result.HostItems {
			hosts = append(hosts, finding.Host)
		}
		if index == 0 {
			expectedHosts = hosts
			if !reflect.DeepEqual(expectedHosts, []string{
				"host-interval.example",
				"very-long-recorded-host-name-for-route-transition-review.example",
				"host-mixed-recent.example",
				"host-direct-to-proxy.example",
			}) {
				t.Fatalf("anchor %s: host findings=%v, want exact intended set/order", anchor.Format(time.RFC3339), hosts)
			}
			continue
		}
		if !reflect.DeepEqual(hosts, expectedHosts) {
			t.Fatalf("anchor %s: host findings=%v differ from first anchor=%v", anchor.Format(time.RFC3339), hosts, expectedHosts)
		}
	}
}

func routeShiftBaselineBucket(anchor time.Time) *time.Time {
	recentBucket := anchor.UTC().Truncate(time.Hour).Add(-time.Hour)
	baselineBucketLocal := recentBucket.In(time.Local)
	baselineBucketLocal = time.Date(
		baselineBucketLocal.Year(), baselineBucketLocal.Month(), baselineBucketLocal.Day()-1,
		baselineBucketLocal.Hour(), 0, 0, 0, time.Local,
	)
	baselineBucket := baselineBucketLocal.UTC()
	return &baselineBucket
}

func timePtr(value time.Time) *time.Time {
	return &value
}
