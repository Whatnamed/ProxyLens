package storage

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func TestAuditIntelligenceCatalogedBackgroundProcessSemantics(t *testing.T) {
	intervalStart := intelligenceTestAnchor.Add(30 * time.Minute)
	intervalEnd := intelligenceTestAnchor.Add(90 * time.Minute)
	events := []intelligenceFixtureEvent{
		{id: "catalog-ms-old", connectionID: "catalog-ms-old", process: "MsMpEng.exe", processPath: `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.1\MsMpEng.exe`, host: "defender-antivirus.synthetic.example", destinationIP: "203.0.113.101", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteProxy, up: 100, down: 900, observedAt: intelligenceTestAnchor.Add(5 * time.Minute)},
		{id: "catalog-ms-new", connectionID: "catalog-ms-new", process: "msmpeng.EXE", processPath: `C:/ProgramData/Microsoft/Windows Defender/Platform/4.18.2/MSMPENG.EXE`, host: "defender-antivirus.synthetic.example", destinationIP: "203.0.113.101", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteProxy, up: 200, down: 1800, observedAt: intelligenceTestAnchor.Add(10 * time.Minute)},
		{id: "catalog-core", connectionID: "catalog-core", process: "MpDefenderCoreService.exe", processPath: `C:\Program Files\Windows Defender\MpDefenderCoreService.exe`, host: "defender-core.synthetic.example", destinationIP: "203.0.113.102", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteProxy, up: 300, down: 2700, observedAt: intelligenceTestAnchor.Add(15 * time.Minute)},
		{id: "catalog-nis", connectionID: "catalog-nis", process: "NisSrv.exe", processPath: `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.synthetic\NisSrv.exe`, sniffHost: "defender-network.synthetic.example", destinationIP: "203.0.113.103", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteProxy, up: 1000, down: 3000, observedAt: intervalStart, precision: "interval_derived", intervalStart: &intervalStart, intervalEnd: &intervalEnd},
		{id: "catalog-wrong-path", connectionID: "catalog-wrong-path", process: "MsMpEng.exe", processPath: `C:\Users\Synthetic\Downloads\MsMpEng.exe`, host: "wrong-path.synthetic.example", destinationIP: "203.0.113.104", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteProxy, up: 500, down: 500, observedAt: intelligenceTestAnchor.Add(20 * time.Minute)},
		{id: "catalog-missing-path", connectionID: "catalog-missing-path", process: "MsMpEng.exe", host: "missing-path.synthetic.example", destinationIP: "203.0.113.105", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteProxy, up: 600, down: 600, observedAt: intelligenceTestAnchor.Add(21 * time.Minute)},
		{id: "catalog-direct", connectionID: "catalog-direct", process: "NisSrv.exe", processPath: `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.synthetic\NisSrv.exe`, host: "direct.synthetic.example", destinationIP: "203.0.113.106", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteDirect, up: 700, down: 700, observedAt: intelligenceTestAnchor.Add(22 * time.Minute)},
	}
	db, cleanup := buildIntelligenceFixture(t, events, false)
	defer cleanup()

	from := intelligenceTestAnchor
	to := intelligenceTestAnchor.Add(time.Hour)
	result, err := NewAuditIntelligenceService(db).ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
	if err != nil {
		t.Fatalf("ListFindings failed: %v", err)
	}
	if result.KnowledgeCatalogVersion != backgroundProcessCatalogVersion {
		t.Fatalf("catalog version missing: %q", result.KnowledgeCatalogVersion)
	}
	items := findingsOfKind(result.Items, AuditFindingCatalogedBackgroundProcess)
	if len(items) != 3 || result.CountsByKind[AuditFindingCatalogedBackgroundProcess] != 3 {
		t.Fatalf("expected exactly three catalog findings, got items=%d counts=%+v", len(items), result.CountsByKind)
	}

	byProcess := make(map[string]AuditFinding)
	for _, item := range items {
		byProcess[item.Subject.Process] = item
		if item.Knowledge == nil || item.Knowledge.MatchBasis != "process_name_and_path" || len(item.Knowledge.Sources) == 0 {
			t.Fatalf("catalog provenance missing: %+v", item)
		}
		if item.Evidence.Route != types.RouteProxy {
			t.Fatalf("catalog finding route mismatch: %+v", item.Evidence)
		}
	}
	if ms, ok := byProcess["msmpeng.EXE"]; !ok || ms.Subject.TargetValue != "defender-antivirus.synthetic.example" || ms.Subject.ProcessPath != `C:/ProgramData/Microsoft/Windows Defender/Platform/4.18.2/MSMPENG.EXE` {
		t.Fatalf("latest MsMpEng path/target not represented: %+v", byProcess)
	}
	if core, ok := byProcess["MpDefenderCoreService.exe"]; !ok || core.Subject.TargetValue != "defender-core.synthetic.example" {
		t.Fatalf("Defender Core finding missing: %+v", byProcess)
	}
	nis, ok := byProcess["NisSrv.exe"]
	if !ok || nis.Subject.TargetKind != "sniff_host" || nis.Subject.TargetValue != "defender-network.synthetic.example" {
		t.Fatalf("NisSrv target precedence mismatch: %+v", byProcess)
	}
	if nis.Evidence.EstimatedUploadBytes == 0 || nis.Evidence.EstimatedDownloadBytes == 0 {
		t.Fatalf("interval-derived catalog evidence was not preserved: %+v", nis.Evidence)
	}
	for _, negative := range []string{"wrong-path.synthetic.example", "missing-path.synthetic.example", "direct.synthetic.example"} {
		for _, item := range items {
			if item.Subject.TargetValue == negative {
				t.Fatalf("negative catalog case appeared: %q", negative)
			}
		}
	}

	earlyTo := intelligenceTestAnchor.Add(8 * time.Minute)
	early, err := NewAuditIntelligenceService(db).ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &earlyTo})
	if err != nil {
		t.Fatalf("early ListFindings failed: %v", err)
	}
	earlyItems := findingsOfKind(early.Items, AuditFindingCatalogedBackgroundProcess)
	if len(earlyItems) != 1 || earlyItems[0].Subject.ProcessPath != `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.1\MsMpEng.exe` {
		t.Fatalf("early catalog path evidence mismatch: %+v", earlyItems)
	}
	if earlyItems[0].ID != byProcess["msmpeng.EXE"].ID {
		t.Fatalf("path enrichment changed finding identity: early=%s late=%s", earlyItems[0].ID, byProcess["msmpeng.EXE"].ID)
	}
}

func TestAuditIntelligenceCatalogedBackgroundProcessLegacyV2Equivalence(t *testing.T) {
	events := []intelligenceFixtureEvent{
		{id: "catalog-equivalent-ms", connectionID: "catalog-equivalent-ms", process: "MsMpEng.exe", processPath: `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.eq\MsMpEng.exe`, host: "equivalent-defender.synthetic.example", destinationIP: "203.0.113.120", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteProxy, up: 123, down: 456, observedAt: intelligenceTestAnchor.Add(5 * time.Minute)},
		{id: "catalog-equivalent-direct", connectionID: "catalog-equivalent-direct", process: "MsMpEng.exe", processPath: `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.eq\MsMpEng.exe`, host: "equivalent-direct.synthetic.example", destinationIP: "203.0.113.121", network: "tcp", rule: "DomainSuffix", payload: "synthetic.example", route: types.RouteDirect, up: 999, down: 999, observedAt: intelligenceTestAnchor.Add(6 * time.Minute)},
	}
	legacyDB, legacyCleanup := buildIntelligenceFixture(t, events, false)
	defer legacyCleanup()
	v2DB, v2Cleanup := buildIntelligenceFixture(t, events, true)
	defer v2Cleanup()
	from := intelligenceTestAnchor
	to := intelligenceTestAnchor.Add(time.Hour)
	legacy, err := NewAuditIntelligenceService(legacyDB).ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
	if err != nil {
		t.Fatalf("legacy findings failed: %v", err)
	}
	v2, err := NewAuditIntelligenceService(v2DB).ListFindings(context.Background(), AuditFindingFilter{StartTime: &from, EndTime: &to})
	if err != nil {
		t.Fatalf("v2 findings failed: %v", err)
	}
	if !reflect.DeepEqual(findingsOfKind(legacy.Items, AuditFindingCatalogedBackgroundProcess), findingsOfKind(v2.Items, AuditFindingCatalogedBackgroundProcess)) {
		t.Fatalf("legacy/v2 catalog findings differ: legacy=%+v v2=%+v", legacy.Items, v2.Items)
	}
}
