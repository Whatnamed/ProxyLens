package storage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBackgroundCatalogParsesBuiltInV1(t *testing.T) {
	catalog, err := builtInBackgroundProcessCatalog()
	if err != nil {
		t.Fatalf("built-in catalog failed to parse: %v", err)
	}
	if catalog.Version != backgroundProcessCatalogVersion || len(catalog.Entries) != 3 {
		t.Fatalf("unexpected built-in catalog: version=%q entries=%d", catalog.Version, len(catalog.Entries))
	}
	for _, process := range []string{"MsMpEng.exe", "MpDefenderCoreService.exe", "NisSrv.exe"} {
		if catalog.byProcessName[normalizeBackgroundProcessName(process)] == nil {
			t.Fatalf("built-in catalog missing process %q", process)
		}
	}
}

func TestBackgroundCatalogRejectsDuplicateEntryID(t *testing.T) {
	catalog := builtInCatalogCopy(t)
	duplicate := catalog.Entries[0]
	duplicate.ProcessNames = []string{"duplicate.exe"}
	catalog.Entries = append(catalog.Entries, duplicate)
	assertCatalogRejected(t, catalog, "duplicate entry ID")
}

func TestBackgroundCatalogRejectsDuplicateProcessNameAcrossEntries(t *testing.T) {
	catalog := builtInCatalogCopy(t)
	catalog.Entries[1].ProcessNames = []string{catalog.Entries[0].ProcessNames[0]}
	assertCatalogRejected(t, catalog, "duplicate process name")
}

func TestBackgroundCatalogRejectsUnknownSource(t *testing.T) {
	catalog := builtInCatalogCopy(t)
	catalog.Entries[0].SourceIDs = append(catalog.Entries[0].SourceIDs, "missing-source")
	assertCatalogRejected(t, catalog, "unknown source")
}

func TestBackgroundCatalogRejectsInvalidPathRule(t *testing.T) {
	catalog := builtInCatalogCopy(t)
	catalog.Entries[0].PathRules[0].Kind = "regex"
	assertCatalogRejected(t, catalog, "unsupported path rule")
}

func TestBackgroundCatalogRejectsNonHTTPSProvenance(t *testing.T) {
	catalog := builtInCatalogCopy(t)
	source := catalog.Sources["microsoft-defender-processes"]
	source.URL = "http://example.invalid/source"
	catalog.Sources["microsoft-defender-processes"] = source
	assertCatalogRejected(t, catalog, "non-https source")
}

func TestNormalizeWindowsObservedPath(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{name: "slashes and case", input: ` C:/ProgramData//Microsoft/Windows Defender/Platform/ `, want: `c:\programdata\microsoft\windows defender\platform`},
		{name: "quoted", input: `"C:\Program Files\Windows Defender\NisSrv.exe"`, want: `c:\program files\windows defender\nissrv.exe`},
		{name: "drive root", input: `C:\`, want: `c:\`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeWindowsObservedPath(tt.input); got != tt.want {
				t.Fatalf("normalizeWindowsObservedPath(%q)=%q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBackgroundCatalogExactAndUnderDirectoryMatches(t *testing.T) {
	catalog, err := builtInBackgroundProcessCatalog()
	if err != nil {
		t.Fatal(err)
	}
	positive := []struct {
		process string
		path    string
	}{
		{"MsMpEng.exe", `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.999\MsMpEng.exe`},
		{"msmpeng.EXE", `c:/programdata/microsoft/windows defender/platform/4.18.999/MSMPENG.EXE`},
		{"MpDefenderCoreService.exe", `C:\Program Files\Windows Defender\MpDefenderCoreService.exe`},
		{"NisSrv.exe", `C:\ProgramData\Microsoft\Windows Defender\Platform\4.18.synthetic\NisSrv.exe`},
	}
	for _, tt := range positive {
		if match := catalog.Match(tt.process, tt.path); match == nil {
			t.Fatalf("expected catalog match for process=%q path=%q", tt.process, tt.path)
		}
	}
}

func TestBackgroundCatalogRejectsWrongPathWithSameProcessName(t *testing.T) {
	catalog, err := builtInBackgroundProcessCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if match := catalog.Match("MsMpEng.exe", `C:\Users\Synthetic\Downloads\MsMpEng.exe`); match != nil {
		t.Fatalf("wrong-path process unexpectedly matched: %+v", match)
	}
	if match := catalog.Match("MsMpEng.exe", ""); match != nil {
		t.Fatalf("pathless process unexpectedly matched: %+v", match)
	}
}

func builtInCatalogCopy(t *testing.T) BackgroundProcessCatalog {
	t.Helper()
	var catalog BackgroundProcessCatalog
	if err := json.Unmarshal(backgroundProcessCatalogJSON, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func assertCatalogRejected(t *testing.T, catalog BackgroundProcessCatalog, label string) {
	t.Helper()
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal %s fixture: %v", label, err)
	}
	if _, err := parseBackgroundProcessCatalog(data); err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Fatalf("expected %s catalog to be rejected", label)
	}
}
