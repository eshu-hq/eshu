// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fileFactWithImports builds one "file" fact envelope carrying the parser's
// per-file imports bucket, the shape the git collector emits for every parsed
// source file.
func fileFactWithImports(factID, relPath, language string, imports []map[string]any) facts.Envelope {
	entries := make([]any, len(imports))
	for i := range imports {
		entries[i] = imports[i]
	}
	return facts.Envelope{
		FactID:   factID,
		ScopeID:  "scope-1",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-abc",
			"relative_path": relPath,
			"language":      language,
			"parsed_file_data": map[string]any{
				"imports": entries,
			},
		},
	}
}

func importRepositoryFact() facts.Envelope {
	return facts.Envelope{
		FactID:   "r-1",
		ScopeID:  "scope-1",
		FactKind: "repository",
		Payload: map[string]any{
			"repo_id": "repo-abc",
			"path":    "/repos/my-project",
		},
	}
}

type importKey struct {
	file, module string
}

func importRowsByEndpoints(t *testing.T, rows []ImportRow) map[importKey]ImportRow {
	t.Helper()
	byKey := make(map[importKey]ImportRow, len(rows))
	for _, row := range rows {
		key := importKey{row.FilePath, row.ModuleName}
		if _, duplicate := byKey[key]; duplicate {
			t.Fatalf("two rows share the endpoints %+v; the backend can only hold one edge for them", key)
		}
		byKey[key] = row
	}
	return byKey
}

// TestBuildCanonicalMaterializationExtractsImportsFromParsedFileData is the
// regression guard for issue #5691: before this, no runtime producer populated
// CanonicalMaterialization.Imports, so a freshly indexed stack carried zero
// File-[:IMPORTS]->Module edges and symbol_graph.imports answered empty with
// confidence. The parser has always written the per-file "imports" bucket into
// parsed_file_data; the projector simply never read it.
func TestBuildCanonicalMaterializationExtractsImportsFromParsedFileData(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		importRepositoryFact(),
		// Go: the module path lands in "name" and there is no "source" key.
		fileFactWithImports("f-go", "cmd/api/main.go", "go", []map[string]any{
			{"name": "fmt", "line_number": 4, "lang": "go"},
			{"name": "github.com/acme/lib-common/log", "line_number": 6, "lang": "go", "alias": "logging"},
		}),
		// Python: "source" carries the module, "name" the imported symbol.
		fileFactWithImports("f-py", "src/client.py", "python", []map[string]any{
			{"name": "Session", "source": "requests", "line_number": 3, "lang": "python", "alias": "req"},
			{"name": "os", "source": "os", "line_number": 1, "lang": "python", "alias": "os"},
		}),
		// TypeScript: a single named symbol keeps its name on the edge.
		fileFactWithImports("f-ts", "src/app.ts", "typescript", []map[string]any{
			{"name": "Router", "source": "express", "line_number": 2, "lang": "typescript"},
		}),
	}

	result, quarantined := buildCanonicalMaterialization(testScope(), testGeneration(), envelopes)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}

	got := importRowsByEndpoints(t, result.Imports)

	want := map[importKey]ImportRow{
		{"/repos/my-project/cmd/api/main.go", "fmt"}:                            {ImportedName: "", Alias: "", LineNumber: 4},
		{"/repos/my-project/cmd/api/main.go", "github.com/acme/lib-common/log"}: {ImportedName: "", Alias: "logging", LineNumber: 6},
		{"/repos/my-project/src/client.py", "requests"}:                         {ImportedName: "Session", Alias: "req", LineNumber: 3},
		{"/repos/my-project/src/client.py", "os"}:                               {ImportedName: "", Alias: "os", LineNumber: 1},
		{"/repos/my-project/src/app.ts", "express"}:                             {ImportedName: "Router", Alias: "", LineNumber: 2},
	}

	if len(result.Imports) != len(want) {
		t.Fatalf("len(Imports) = %d, want %d: %+v", len(result.Imports), len(want), result.Imports)
	}
	for key, expected := range want {
		row, ok := got[key]
		if !ok {
			t.Errorf("missing import row %+v", key)
			continue
		}
		if row.ImportedName != expected.ImportedName {
			t.Errorf("%+v imported_name = %q, want %q", key, row.ImportedName, expected.ImportedName)
		}
		if row.Alias != expected.Alias {
			t.Errorf("%+v alias = %q, want %q", key, row.Alias, expected.Alias)
		}
		if row.LineNumber != expected.LineNumber {
			t.Errorf("%+v line_number = %d, want %d", key, row.LineNumber, expected.LineNumber)
		}
	}

	// Every imported module must also materialize a Module node, or the
	// IMPORTS edge writer's MATCH (m:Module) finds nothing and the edge is
	// silently dropped.
	modules := make(map[string]string, len(result.Modules))
	for _, m := range result.Modules {
		modules[m.Name] = m.Language
	}
	for _, name := range []string{"fmt", "github.com/acme/lib-common/log", "requests", "os", "express"} {
		if _, ok := modules[name]; !ok {
			t.Errorf("Module %q not materialized; have %+v", name, result.Modules)
		}
	}
	if lang := modules["fmt"]; lang != "go" {
		t.Errorf("Module fmt language = %q, want go", lang)
	}
	if lang := modules["express"]; lang != "typescript" {
		t.Errorf("Module express language = %q, want typescript", lang)
	}
}

// TestBuildCanonicalMaterializationFoldsMultiSymbolImportsHonestly pins the
// behavior that follows from the backend's edge identity: `import { Router,
// json } from "express"` is ONE File->Module edge, because the pinned NornicDB
// build does not treat a relationship property as part of MERGE identity. Since
// the edge cannot say which symbol it carries, it must say nothing rather than
// name whichever row the batch happened to write last.
func TestBuildCanonicalMaterializationFoldsMultiSymbolImportsHonestly(t *testing.T) {
	t.Parallel()

	result, _ := buildCanonicalMaterialization(testScope(), testGeneration(), []facts.Envelope{
		importRepositoryFact(),
		fileFactWithImports("f-ts", "src/app.ts", "typescript", []map[string]any{
			{"name": "Router", "source": "express", "line_number": 9, "alias": "R"},
			{"name": "json", "source": "express", "line_number": 2},
		}),
	})

	if len(result.Imports) != 1 {
		t.Fatalf("len(Imports) = %d, want 1: %+v", len(result.Imports), result.Imports)
	}
	row := result.Imports[0]
	if row.ImportedName != "" {
		t.Errorf("imported_name = %q, want empty: two symbols share this edge and neither may claim it", row.ImportedName)
	}
	if row.Alias != "" {
		t.Errorf("alias = %q, want empty for the same reason", row.Alias)
	}
	if row.LineNumber != 2 {
		t.Errorf("line_number = %d, want 2 (earliest attributed line)", row.LineNumber)
	}
}

// TestBuildCanonicalMaterializationSkipsUnusableImportEntries keeps malformed
// or empty parser entries from minting an anonymous Module node. An import
// with no resolvable module name is not truth we can write.
func TestBuildCanonicalMaterializationSkipsUnusableImportEntries(t *testing.T) {
	t.Parallel()

	result, _ := buildCanonicalMaterialization(testScope(), testGeneration(), []facts.Envelope{
		importRepositoryFact(),
		fileFactWithImports("f-go", "main.go", "go", []map[string]any{
			{"line_number": 4},
			{"name": "   ", "line_number": 5},
			{"name": "fmt", "line_number": 6},
		}),
	})

	if len(result.Imports) != 1 {
		t.Fatalf("len(Imports) = %d, want 1: %+v", len(result.Imports), result.Imports)
	}
	if result.Imports[0].ModuleName != "fmt" {
		t.Errorf("ModuleName = %q, want fmt", result.Imports[0].ModuleName)
	}
	for _, m := range result.Modules {
		if m.Name == "" {
			t.Errorf("materialized an empty-named Module node: %+v", result.Modules)
		}
	}
}

// TestBuildCanonicalMaterializationIgnoresTombstonedFileImports keeps a deleted
// file's imports out of the generation's edge set.
func TestBuildCanonicalMaterializationIgnoresTombstonedFileImports(t *testing.T) {
	t.Parallel()

	tombstoned := fileFactWithImports("f-go", "main.go", "go", []map[string]any{
		{"name": "fmt", "line_number": 4},
	})
	tombstoned.IsTombstone = true

	result, _ := buildCanonicalMaterialization(testScope(), testGeneration(), []facts.Envelope{
		importRepositoryFact(),
		tombstoned,
	})

	if len(result.Imports) != 0 {
		t.Fatalf("len(Imports) = %d, want 0: %+v", len(result.Imports), result.Imports)
	}
}

// TestBuildCanonicalMaterializationImportRowsAreOrderStable guards the batch
// the writer sends: the same generation projected twice must produce the same
// row order, or a golden snapshot diff stops meaning anything.
func TestBuildCanonicalMaterializationImportRowsAreOrderStable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		importRepositoryFact(),
		fileFactWithImports("f-a", "a.go", "go", []map[string]any{
			{"name": "fmt", "line_number": 3},
			{"name": "os", "line_number": 4},
			{"name": "strings", "line_number": 5},
		}),
		fileFactWithImports("f-b", "b.py", "python", []map[string]any{
			{"name": "Session", "source": "requests", "line_number": 1},
			{"name": "Path", "source": "pathlib", "line_number": 2},
		}),
	}

	first, _ := buildCanonicalMaterialization(testScope(), testGeneration(), envelopes)
	for i := 0; i < 5; i++ {
		again, _ := buildCanonicalMaterialization(testScope(), testGeneration(), envelopes)
		if len(again.Imports) != len(first.Imports) {
			t.Fatalf("run %d: len(Imports) = %d, want %d", i, len(again.Imports), len(first.Imports))
		}
		for j := range first.Imports {
			if again.Imports[j] != first.Imports[j] {
				t.Fatalf("run %d: Imports[%d] = %+v, want %+v", i, j, again.Imports[j], first.Imports[j])
			}
		}
		for j := range first.Modules {
			if again.Modules[j] != first.Modules[j] {
				t.Fatalf("run %d: Modules[%d] = %+v, want %+v", i, j, again.Modules[j], first.Modules[j])
			}
		}
	}
}
