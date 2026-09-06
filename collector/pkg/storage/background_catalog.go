package storage

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

const backgroundProcessCatalogVersion = "background-processes-v1"

//go:embed knowledge/background-processes-v1.json
var backgroundProcessCatalogJSON []byte

type BackgroundProcessCatalog struct {
	Version    string                               `json:"version"`
	ReviewedAt string                               `json:"reviewedAt"`
	Sources    map[string]BackgroundKnowledgeSource `json:"sources"`
	Entries    []BackgroundProcessCatalogEntry      `json:"entries"`

	byProcessName map[string]*BackgroundProcessCatalogEntry
}

type BackgroundKnowledgeSource struct {
	Publisher string `json:"publisher"`
	Title     string `json:"title"`
	URL       string `json:"url"`
}

type BackgroundProcessCatalogEntry struct {
	ID           string                      `json:"id"`
	Category     string                      `json:"category"`
	Publisher    string                      `json:"publisher"`
	Family       string                      `json:"family"`
	ProcessNames []string                    `json:"processNames"`
	PathRules    []BackgroundProcessPathRule `json:"pathRules"`
	SourceIDs    []string                    `json:"sourceIds"`
}

type BackgroundProcessPathRule struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Directory string `json:"directory,omitempty"`
}

type BackgroundProcessMatch struct {
	CatalogVersion string
	Entry          *BackgroundProcessCatalogEntry
	Process        string
	ProcessPath    string
	MatchBasis     string
}

var (
	backgroundProcessCatalogOnce sync.Once
	backgroundProcessCatalog     *BackgroundProcessCatalog
	backgroundProcessCatalogErr  error
)

func builtInBackgroundProcessCatalog() (*BackgroundProcessCatalog, error) {
	backgroundProcessCatalogOnce.Do(func() {
		backgroundProcessCatalog, backgroundProcessCatalogErr = parseBackgroundProcessCatalog(backgroundProcessCatalogJSON)
	})
	return backgroundProcessCatalog, backgroundProcessCatalogErr
}

func parseBackgroundProcessCatalog(data []byte) (*BackgroundProcessCatalog, error) {
	var catalog BackgroundProcessCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parse background process catalog: %w", err)
	}
	if catalog.Version != backgroundProcessCatalogVersion {
		return nil, fmt.Errorf("unsupported background process catalog version %q", catalog.Version)
	}
	if strings.TrimSpace(catalog.ReviewedAt) == "" {
		return nil, fmt.Errorf("background process catalog reviewedAt is required")
	}
	if len(catalog.Sources) == 0 {
		return nil, fmt.Errorf("background process catalog requires at least one source")
	}
	for sourceID, source := range catalog.Sources {
		if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(source.Publisher) == "" || strings.TrimSpace(source.Title) == "" {
			return nil, fmt.Errorf("background process catalog source %q is incomplete", sourceID)
		}
		parsed, err := url.Parse(strings.TrimSpace(source.URL))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, fmt.Errorf("background process catalog source %q must use an https URL", sourceID)
		}
	}
	if len(catalog.Entries) == 0 {
		return nil, fmt.Errorf("background process catalog requires at least one entry")
	}

	entryIDs := make(map[string]struct{}, len(catalog.Entries))
	processNames := make(map[string]string)
	catalog.byProcessName = make(map[string]*BackgroundProcessCatalogEntry)
	for index := range catalog.Entries {
		entry := &catalog.Entries[index]
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Category = strings.TrimSpace(entry.Category)
		entry.Publisher = strings.TrimSpace(entry.Publisher)
		entry.Family = strings.TrimSpace(entry.Family)
		if entry.ID == "" || entry.Category == "" || entry.Publisher == "" || entry.Family == "" {
			return nil, fmt.Errorf("background process catalog entry %d is incomplete", index)
		}
		if _, exists := entryIDs[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate background process catalog entry ID %q", entry.ID)
		}
		entryIDs[entry.ID] = struct{}{}
		if len(entry.ProcessNames) == 0 || len(entry.PathRules) == 0 || len(entry.SourceIDs) == 0 {
			return nil, fmt.Errorf("background process catalog entry %q requires process names, path rules, and sources", entry.ID)
		}
		seenEntryNames := make(map[string]struct{}, len(entry.ProcessNames))
		for nameIndex, processName := range entry.ProcessNames {
			processName = strings.TrimSpace(processName)
			normalizedName := normalizeBackgroundProcessName(processName)
			if normalizedName == "" {
				return nil, fmt.Errorf("background process catalog entry %q process name %d is empty", entry.ID, nameIndex)
			}
			if _, exists := seenEntryNames[normalizedName]; exists {
				return nil, fmt.Errorf("duplicate process name %q in entry %q", processName, entry.ID)
			}
			seenEntryNames[normalizedName] = struct{}{}
			if otherID, exists := processNames[normalizedName]; exists && otherID != entry.ID {
				return nil, fmt.Errorf("process name %q appears in entries %q and %q", processName, otherID, entry.ID)
			}
			processNames[normalizedName] = entry.ID
			entry.ProcessNames[nameIndex] = processName
			catalog.byProcessName[normalizedName] = entry
		}
		seenSources := make(map[string]struct{}, len(entry.SourceIDs))
		for _, sourceID := range entry.SourceIDs {
			sourceID = strings.TrimSpace(sourceID)
			if sourceID == "" {
				return nil, fmt.Errorf("background process catalog entry %q has an empty source ID", entry.ID)
			}
			if _, exists := catalog.Sources[sourceID]; !exists {
				return nil, fmt.Errorf("background process catalog entry %q references unknown source %q", entry.ID, sourceID)
			}
			if _, exists := seenSources[sourceID]; exists {
				return nil, fmt.Errorf("background process catalog entry %q repeats source %q", entry.ID, sourceID)
			}
			seenSources[sourceID] = struct{}{}
		}
		for ruleIndex, rule := range entry.PathRules {
			kind := strings.TrimSpace(rule.Kind)
			switch kind {
			case "exact":
				if normalizeWindowsObservedPath(rule.Path) == "" || strings.TrimSpace(rule.Directory) != "" {
					return nil, fmt.Errorf("background process catalog entry %q path rule %d has invalid exact fields", entry.ID, ruleIndex)
				}
			case "under_directory":
				if normalizeWindowsObservedPath(rule.Directory) == "" || strings.TrimSpace(rule.Path) != "" {
					return nil, fmt.Errorf("background process catalog entry %q path rule %d has invalid under_directory fields", entry.ID, ruleIndex)
				}
			default:
				return nil, fmt.Errorf("background process catalog entry %q uses unsupported path rule %q", entry.ID, kind)
			}
			entry.PathRules[ruleIndex].Kind = kind
		}
	}
	return &catalog, nil
}

func normalizeBackgroundProcessName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeWindowsObservedPath(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	value = strings.ReplaceAll(value, "/", "\\")
	var builder strings.Builder
	lastWasSeparator := false
	for _, r := range value {
		if r == '\\' {
			if lastWasSeparator {
				continue
			}
			lastWasSeparator = true
		} else {
			lastWasSeparator = false
		}
		builder.WriteRune(r)
	}
	value = builder.String()
	if len(value) > 3 && strings.HasSuffix(value, "\\") {
		value = strings.TrimRight(value, "\\")
	}
	return strings.ToLower(value)
}

func (c *BackgroundProcessCatalog) Match(process, processPath string) *BackgroundProcessMatch {
	if c == nil {
		return nil
	}
	entry := c.byProcessName[normalizeBackgroundProcessName(process)]
	if entry == nil || normalizeWindowsObservedPath(processPath) == "" {
		return nil
	}
	normalizedPath := normalizeWindowsObservedPath(processPath)
	normalizedProcess := normalizeBackgroundProcessName(process)
	for _, rule := range entry.PathRules {
		matched := false
		switch rule.Kind {
		case "exact":
			matched = normalizedPath == normalizeWindowsObservedPath(rule.Path)
		case "under_directory":
			directory := normalizeWindowsObservedPath(rule.Directory)
			matched = strings.HasPrefix(normalizedPath, directory+"\\") && pathBaseWindows(normalizedPath) == normalizedProcess
		}
		if matched {
			return &BackgroundProcessMatch{
				CatalogVersion: c.Version,
				Entry:          entry,
				Process:        process,
				ProcessPath:    processPath,
				MatchBasis:     "process_name_and_path",
			}
		}
	}
	return nil
}

func pathBaseWindows(value string) string {
	if index := strings.LastIndexByte(value, '\\'); index >= 0 {
		return value[index+1:]
	}
	return value
}
