// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// canonical_import_edge_live_test.go is the backend-required proof for issue
// #5691: File-[:IMPORTS]->Module edges, which had no producer at all until that
// issue, now reach a real NornicDB through the production CanonicalNodeWriter
// and the real PhaseGroupExecutor dispatch.
//
// It is backend-required rather than a Cypher string assertion because the
// projector's fold — one edge per (file, module), with a per-symbol property
// carried only when every entry agrees — is a consequence of how the backend
// stores relationship identity, not of the Cypher text. On the pinned build a
// relationship property map in a MERGE pattern is NOT part of identity (see
// docs/public/reference/nornicdb-pitfalls.md), so a per-symbol edge set is not
// representable and an extractor that emitted one row per symbol would lose
// rows silently at write time. Only a real backend can hold that line.
//
// Skills active: golang-engineering, cypher-query-rigor,
// eshu-diagnostic-rigor.

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	importEdgeRepoID   = "5691-import-edges"
	importEdgeRepoPath = "/repos/5691-import-edges"
)

func importEdgeCleanup(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmts := []struct {
		cypher string
		params map[string]any
	}{
		{`MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`, map[string]any{"repo_id": importEdgeRepoID}},
		{`MATCH (d:Directory) WHERE d.repo_id = $repo_id DETACH DELETE d`, map[string]any{"repo_id": importEdgeRepoID}},
		{`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`, map[string]any{"repo_id": importEdgeRepoID}},
		{`MATCH (m:Module) WHERE m.name IN $names DETACH DELETE m`, map[string]any{"names": []any{"express", "fmt"}}},
	}
	for _, s := range stmts {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: s.cypher, Parameters: s.params}); err != nil {
			t.Fatalf("cleanup %q: %v", s.cypher, err)
		}
	}
}

func importEdgeMaterialization(generationID string, first bool, imports []projector.ImportRow) projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:         "git:repository:" + importEdgeRepoID,
		GenerationID:    generationID,
		RepoID:          importEdgeRepoID,
		RepoPath:        importEdgeRepoPath,
		FirstGeneration: first,
		Repository:      &projector.RepositoryRow{RepoID: importEdgeRepoID, Name: importEdgeRepoID, Path: importEdgeRepoPath},
		Directories: []projector.DirectoryRow{
			{Path: importEdgeRepoPath + "/src", Name: "src", ParentPath: importEdgeRepoPath, RepoID: importEdgeRepoID, Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: importEdgeRepoPath + "/src/app.ts", RelativePath: "src/app.ts", Name: "app.ts", Language: "typescript", RepoID: importEdgeRepoID, DirPath: importEdgeRepoPath + "/src"},
			{Path: importEdgeRepoPath + "/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: importEdgeRepoID, DirPath: importEdgeRepoPath + "/src"},
		},
		Modules: []projector.ModuleRow{
			{Name: "express", Language: "typescript"},
			{Name: "fmt", Language: "go"},
		},
		Imports: imports,
	}
}

func importEdgeRows() []projector.ImportRow {
	return []projector.ImportRow{
		{FilePath: importEdgeRepoPath + "/src/app.ts", ModuleName: "express", ImportedName: "Router", Alias: "R", LineNumber: 2},
		{FilePath: importEdgeRepoPath + "/src/main.go", ModuleName: "fmt", ImportedName: "", LineNumber: 4},
	}
}

// TestCanonicalImportEdgesGraphTruth proves the whole chain the #5691 producer
// feeds: the writer lands one IMPORTS edge per (file, module) with its
// properties intact, a module-level import with no symbol lands exactly one
// edge, and re-projecting the same generation neither duplicates an edge nor
// drops one.
func TestCanonicalImportEdgesGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the IMPORTS edge proof against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	importEdgeCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		importEdgeCleanup(cleanCtx, t, exec)
	})

	if err := writer.Write(ctx, importEdgeMaterialization("gen1", true, importEdgeRows())); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	assertImportEdgeTruth(ctx, t, exec, "gen1")

	// A second generation re-projects the identical rows. Every edge must be
	// re-MERGEd onto itself, never duplicated and never dropped.
	if err := writer.Write(ctx, importEdgeMaterialization("gen2", false, importEdgeRows())); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	assertImportEdgeTruth(ctx, t, exec, "gen2 re-projection")
}

// assertImportEdgeTruth reads the projected IMPORTS edges back and checks the
// full expected set, not just a count: a count alone would pass if two edges
// existed with the wrong imported_name on each.
func assertImportEdgeTruth(ctx context.Context, t *testing.T, exec liveExecutor, label string) {
	t.Helper()

	rows, err := exec.Run(ctx, `MATCH (f:File)-[r:IMPORTS]->(m:Module)
WHERE f.repo_id = $repo_id
RETURN f.relative_path AS file, m.name AS module, r.imported_name AS imported_name, r.alias AS alias, r.line_number AS line_number`,
		map[string]any{"repo_id": importEdgeRepoID})
	if err != nil {
		t.Fatalf("%s: read IMPORTS edges: %v", label, err)
	}

	type edge struct {
		file, module, imported, alias string
		line                          int64
	}
	got := make([]edge, 0, len(rows))
	for _, row := range rows {
		file, _ := row["file"].(string)
		module, _ := row["module"].(string)
		imported, _ := row["imported_name"].(string)
		alias, _ := row["alias"].(string)
		line, _ := row["line_number"].(int64)
		got = append(got, edge{file, module, imported, alias, line})
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].file != got[j].file {
			return got[i].file < got[j].file
		}
		return got[i].module < got[j].module
	})

	want := []edge{
		{"src/app.ts", "express", "Router", "R", 2},
		{"src/main.go", "fmt", "", "", 4},
	}

	t.Logf("%s: projected IMPORTS edges = %+v", label, got)
	if len(got) != len(want) {
		t.Fatalf("%s: IMPORTS edge count = %d, want %d: %+v", label, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: edge[%d] = %+v, want %+v", label, i, got[i], want[i])
		}
	}
}
