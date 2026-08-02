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

func repositoryFact() facts.Envelope {
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

// TestBuildCanonicalMaterializationExtractsImportsFromParsedFileData is the
// regression guard for issue #5691: before this, no runtime producer populated
// CanonicalMaterialization.Imports, so a freshly indexed stack carried zero
// File-[:IMPORTS]->Module edges and symbol_graph.imports answered empty with
// confidence. The parser has always written the per-file "imports" bucket into
// parsed_file_data; the projector simply never read it.
func TestBuildCanonicalMaterializationExtractsImportsFromParsedFileData(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		repositoryFact(),
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
		// TypeScript: one module, two named symbols -> two distinct edges.
		fileFactWithImports("f-ts", "src/app.ts", "typescript", []map[string]any{
			{"name": "Router", "source": "express", "line_number": 2, "lang": "typescript"},
			{"name": "json", "source": "express", "line_number": 2, "lang": "typescript"},
		}),
	}

	result, quarantined := buildCanonicalMaterialization(testScope(), testGeneration(), envelopes)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}

	type key struct {
		file, module, imported string
	}
	got := make(map[key]ImportRow, len(result.Imports))
	for _, row := range result.Imports {
		got[key{row.FilePath, row.ModuleName, row.ImportedName}] = row
	}

	want := []struct {
		key   key
		alias string
		line  int
	}{
		{key{"/repos/my-project/cmd/api/main.go", "fmt", ""}, "", 4},
		{key{"/repos/my-project/cmd/api/main.go", "github.com/acme/lib-common/log", ""}, "logging", 6},
		{key{"/repos/my-project/src/client.py", "requests", "Session"}, "req", 3},
		{key{"/repos/my-project/src/client.py", "os", ""}, "os", 1},
		{key{"/repos/my-project/src/app.ts", "express", "Router"}, "", 2},
		{key{"/repos/my-project/src/app.ts", "express", "json"}, "", 2},
	}

	if len(result.Imports) != len(want) {
		t.Fatalf("len(Imports) = %d, want %d: %+v", len(result.Imports), len(want), result.Imports)
	}
	for _, w := range want {
		row, ok := got[w.key]
		if !ok {
			t.Errorf("missing import row %+v", w.key)
			continue
		}
		if row.Alias != w.alias {
			t.Errorf("%+v alias = %q, want %q", w.key, row.Alias, w.alias)
		}
		if row.LineNumber != w.line {
			t.Errorf("%+v line_number = %d, want %d", w.key, row.LineNumber, w.line)
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

// TestBuildCanonicalMaterializationDeduplicatesRepeatedImports pins the edge
// identity: the IMPORTS writer MERGEs on (file, module, imported_name), so two
// parser entries that collapse to the same identity must produce ONE row with
// a deterministic line number (the first occurrence) rather than two rows that
// race to overwrite each other's properties.
func TestBuildCanonicalMaterializationDeduplicatesRepeatedImports(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		repositoryFact(),
		fileFactWithImports("f-ts", "src/app.ts", "typescript", []map[string]any{
			{"name": "Router", "source": "express", "line_number": 9},
			{"name": "Router", "source": "express", "line_number": 2},
		}),
	}

	result, _ := buildCanonicalMaterialization(testScope(), testGeneration(), envelopes)

	if len(result.Imports) != 1 {
		t.Fatalf("len(Imports) = %d, want 1: %+v", len(result.Imports), result.Imports)
	}
	if got := result.Imports[0].LineNumber; got != 2 {
		t.Errorf("LineNumber = %d, want 2 (first occurrence wins)", got)
	}
}

// TestBuildCanonicalMaterializationSkipsUnusableImportEntries keeps malformed
// or empty parser entries from minting an anonymous Module node. An import
// with no resolvable module name is not truth we can write.
func TestBuildCanonicalMaterializationSkipsUnusableImportEntries(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		repositoryFact(),
		fileFactWithImports("f-go", "main.go", "go", []map[string]any{
			{"line_number": 4},
			{"name": "   ", "line_number": 5},
			{"name": "fmt", "line_number": 6},
		}),
	}

	result, _ := buildCanonicalMaterialization(testScope(), testGeneration(), envelopes)

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
		repositoryFact(),
		tombstoned,
	})

	if len(result.Imports) != 0 {
		t.Fatalf("len(Imports) = %d, want 0: %+v", len(result.Imports), result.Imports)
	}
}
