// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverQueryCallsitesRecordsExactProductionSymbolsAndCounts(t *testing.T) {
	dir := t.TempDir()
	source := `package query

type Handler struct{ graph Graph }

var packageQuery = func(graph Graph) {
	graph.Run(nil, "RETURN 0", nil)
}

func (h *Handler) handle() {
	h.graph.Run(nil, "RETURN 1", nil)
	h.graph.RunSingle(nil, "RETURN 2", nil)
}

func support(graph Graph) {
	graph.Run(nil, "RETURN 3", nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "handler_test.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write test fixture: %v", err)
	}
	nestedDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatalf("create nested source fixture directory: %v", err)
	}
	nestedSource := `package nested

func query(graph Graph) {
	graph.Run(nil, "RETURN 4", nil)
}
`
	if err := os.WriteFile(filepath.Join(nestedDir, "handler.go"), []byte(nestedSource), 0o600); err != nil {
		t.Fatalf("write nested source fixture: %v", err)
	}
	testdataDir := filepath.Join(dir, "testdata")
	if err := os.MkdirAll(testdataDir, 0o700); err != nil {
		t.Fatalf("create testdata fixture directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testdataDir, "ignored.go"), []byte(nestedSource), 0o600); err != nil {
		t.Fatalf("write ignored testdata fixture: %v", err)
	}

	got, err := DiscoverQueryCallsites(dir)
	if err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v", err)
	}
	want := []SourceCoverage{
		{
			File: "handler.go",
			Calls: []QueryCallsite{
				{Symbol: "(*Handler).handle", Count: 2},
				{Symbol: "<package-init>", Count: 1},
				{Symbol: "support", Count: 1},
			},
		},
		{
			File: filepath.ToSlash(filepath.Join("nested", "handler.go")),
			Calls: []QueryCallsite{
				{Symbol: "query", Count: 1},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverQueryCallsites() = %#v, want %#v", got, want)
	}
}

func TestValidateSourceCoverageRejectsUnregisteredQueryCalls(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		SourceCoverage: []SourceCoverage{
			{
				File: "registered.go",
				Calls: []QueryCallsite{
					{
						Symbol: "(*Handler).registered",
						Count:  1,
						Reason: "bounded support lookup",
					},
				},
			},
		},
	}
	discovered := []SourceCoverage{
		{
			File: "registered.go",
			Calls: []QueryCallsite{
				{Symbol: "(*Handler).registered", Count: 1},
			},
		},
		{
			File: "new_handler.go",
			Calls: []QueryCallsite{
				{Symbol: "(*Handler).newHandler", Count: 1},
			},
		},
	}

	err := ValidateSourceCoverage(manifest, discovered)
	if err == nil || !strings.Contains(err.Error(), "new_handler.go:(*Handler).newHandler") {
		t.Fatalf("ValidateSourceCoverage() error = %v, want unregistered callsite", err)
	}
}

func TestValidateSourceCoverageRejectsChangedCallCount(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		SourceCoverage: []SourceCoverage{
			{
				File: "handler.go",
				Calls: []QueryCallsite{
					{
						Symbol: "(*Handler).handle",
						Count:  1,
						Reason: "bounded support lookup",
					},
				},
			},
		},
	}
	discovered := []SourceCoverage{
		{
			File: "handler.go",
			Calls: []QueryCallsite{
				{Symbol: "(*Handler).handle", Count: 2},
			},
		},
	}

	err := ValidateSourceCoverage(manifest, discovered)
	if err == nil || !strings.Contains(err.Error(), "call count 2, manifest requires 1") {
		t.Fatalf("ValidateSourceCoverage() error = %v, want changed count", err)
	}
}

func TestValidateSourceCoverageRequiresDisposition(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		SourceCoverage: []SourceCoverage{
			{
				File: "handler.go",
				Calls: []QueryCallsite{
					{Symbol: "(*Handler).handle", Count: 1},
				},
			},
		},
	}
	discovered := []SourceCoverage{
		{
			File: "handler.go",
			Calls: []QueryCallsite{
				{Symbol: "(*Handler).handle", Count: 1},
			},
		},
	}

	err := ValidateSourceCoverage(manifest, discovered)
	if err == nil || !strings.Contains(err.Error(), "requires entry_ids or a non-hot reason") {
		t.Fatalf("ValidateSourceCoverage() error = %v, want missing disposition", err)
	}
}

func TestValidateSourceCoverageRejectsUnknownHotPath(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		SourceCoverage: []SourceCoverage{
			{
				File: "handler.go",
				Calls: []QueryCallsite{
					{
						Symbol:   "(*Handler).handle",
						Count:    1,
						EntryIDs: []string{"QP-MISSING"},
					},
				},
			},
		},
	}
	discovered := []SourceCoverage{
		{
			File: "handler.go",
			Calls: []QueryCallsite{
				{Symbol: "(*Handler).handle", Count: 1},
			},
		},
	}

	err := ValidateSourceCoverage(manifest, discovered)
	if err == nil || !strings.Contains(err.Error(), "unknown hot path QP-MISSING") {
		t.Fatalf("ValidateSourceCoverage() error = %v, want unknown hot path", err)
	}
}

func TestValidateSourceCoverageRejectsStaleRegistration(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		SourceCoverage: []SourceCoverage{
			{
				File: "removed.go",
				Calls: []QueryCallsite{
					{
						Symbol: "removed",
						Count:  1,
						Reason: "bounded support lookup",
					},
				},
			},
		},
	}

	err := ValidateSourceCoverage(manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "stale query callsite registration removed.go:removed") {
		t.Fatalf("ValidateSourceCoverage() error = %v, want stale registration", err)
	}
}

func TestHotCypherManifestCoversEveryProductionQueryCall(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	handlerManifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile(handler hot Cypher) error = %v", err)
	}
	manifest.Entries = append(manifest.Entries, handlerManifest.Entries...)
	coverageManifest, err := LoadManifestFile("testdata/query-source-coverage.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile(query source coverage) error = %v", err)
	}
	if err := ValidateManifest(coverageManifest, nil); err != nil {
		t.Fatalf("ValidateManifest(query source coverage) error = %v", err)
	}
	manifest.SourceCoverage = coverageManifest.SourceCoverage
	cypherEntryIDs := make([]string, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.QueryKind == queryKindCypher {
			cypherEntryIDs = append(cypherEntryIDs, entry.ID)
		}
	}
	assertRequiredEntriesLinkedToCallsites(t, cypherEntryIDs, manifest.SourceCoverage)
	discovered, err := DiscoverQueryCallsites("../query")
	if err != nil {
		t.Fatalf("DiscoverQueryCallsites() error = %v", err)
	}
	if err := ValidateSourceCoverage(manifest, discovered); err != nil {
		t.Fatalf("production query source coverage: %v", err)
	}
}

func assertRequiredEntriesLinkedToCallsites(
	t *testing.T,
	requiredIDs []string,
	coverage []SourceCoverage,
) {
	t.Helper()
	linked := make(map[string]struct{})
	for _, source := range coverage {
		for _, callsite := range source.Calls {
			for _, entryID := range callsite.EntryIDs {
				linked[entryID] = struct{}{}
			}
		}
	}
	for _, entryID := range requiredIDs {
		if _, ok := linked[entryID]; !ok {
			t.Errorf("handler hot path %s is not linked to a production query callsite", entryID)
		}
	}
}
