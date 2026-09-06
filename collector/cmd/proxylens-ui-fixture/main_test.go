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

func TestReviewBackgroundServicesFixtureCatalogSetIsAnchorMinuteIndependent(t *testing.T) {
	anchors := []time.Time{
		time.Date(2026, 9, 6, 13, 5, 0, 0, time.UTC),
		time.Date(2026, 9, 6, 13, 55, 0, 0, time.UTC),
	}

	var expected []string
	for index, anchor := range anchors {
		dbPath := filepath.Join(t.TempDir(), "fixture_review-background-services.db")
		generateReviewBackgroundServices(context.Background(), dbPath, anchor)

		db, err := storage.OpenReadOnlyDB(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("anchor %s: OpenReadOnlyDB failed: %v", anchor.Format(time.RFC3339), err)
		}
		from := anchor.Add(-4 * time.Hour)
		to := anchor
		result, err := storage.NewAuditIntelligenceService(db).ListFindings(context.Background(), storage.AuditFindingFilter{
			StartTime: &from, EndTime: &to, LimitPerKind: 20,
		})
		closeErr := db.Close()
		if err != nil {
			t.Fatalf("anchor %s: ListFindings failed: %v", anchor.Format(time.RFC3339), err)
		}
		if closeErr != nil {
			t.Fatalf("anchor %s: close read-only DB: %v", anchor.Format(time.RFC3339), closeErr)
		}
		items := make([]string, 0)
		for _, finding := range result.Items {
			if finding.Kind == storage.AuditFindingCatalogedBackgroundProcess {
				items = append(items, finding.Subject.Process+" -> "+finding.Subject.TargetKind+":"+finding.Subject.TargetValue)
			}
		}
		if len(items) != 3 {
			t.Fatalf("anchor %s: expected exactly three catalog findings, got %v", anchor.Format(time.RFC3339), items)
		}
		if index == 0 {
			expected = items
			if !reflect.DeepEqual(expected, []string{
				"NisSrv.exe -> sniff_host:defender-network.synthetic.example",
				"MpDefenderCoreService.exe -> host:defender-core.synthetic.example",
				"MsMpEng.exe -> host:defender-antivirus.synthetic.example",
			}) {
				t.Fatalf("anchor %s: catalog findings=%v, want exact intended set/order", anchor.Format(time.RFC3339), items)
			}
			continue
		}
		if !reflect.DeepEqual(items, expected) {
			t.Fatalf("anchor %s: catalog findings=%v differ from first anchor=%v", anchor.Format(time.RFC3339), items, expected)
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
