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
// carried only when every entry agrees — is a deliberate Eshu contract rather
// than a backend workaround. The pinned NornicDB v1.3.3 artifact supports property-bearing
// relationship MERGE identity, but this projection must continue to produce
// the same module-level row set. Only a real backend can hold that line.
//
// Skills active: golang-engineering, cypher-query-rigor,
// eshu-diagnostic-rigor.

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
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
		// Module nodes are global -- MERGEd on (name, lang), with no repo_id --
		// so this test uses names no real repository imports, and deletes only
		// those. Cleaning up by a real module name ("fmt", "express") would
		// detach unrelated repositories' import edges on any shared or reused
		// backend, and matching on the name alone would take every language's
		// node with it.
		{`MATCH (m:Module) WHERE m.name IN $names DETACH DELETE m`, map[string]any{"names": []any{"5691-test-express", "5691-test-fmt"}}},
	}
	for _, s := range stmts {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: s.cypher, Parameters: s.params}); err != nil {
			t.Fatalf("cleanup %q: %v", s.cypher, err)
		}
	}
}

func importEdgeMaterialization(generationID string, first bool, imports []canonical.ImportRow) canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:         "git:repository:" + importEdgeRepoID,
		GenerationID:    generationID,
		RepoID:          importEdgeRepoID,
		RepoPath:        importEdgeRepoPath,
		FirstGeneration: first,
		Repository:      &canonical.RepositoryRow{RepoID: importEdgeRepoID, Name: importEdgeRepoID, Path: importEdgeRepoPath},
		Directories: []canonical.DirectoryRow{
			{Path: importEdgeRepoPath + "/src", Name: "src", ParentPath: importEdgeRepoPath, RepoID: importEdgeRepoID, Depth: 0},
		},
		Files: []canonical.FileRow{
			{Path: importEdgeRepoPath + "/src/app.ts", RelativePath: "src/app.ts", Name: "app.ts", Language: "typescript", RepoID: importEdgeRepoID, DirPath: importEdgeRepoPath + "/src"},
			{Path: importEdgeRepoPath + "/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: importEdgeRepoID, DirPath: importEdgeRepoPath + "/src"},
		},
		Modules: []canonical.ModuleRow{
			{Name: "5691-test-express", Language: "typescript"},
			{Name: "5691-test-fmt", Language: "go"},
		},
		Imports: imports,
	}
}

// importEdgeRows carries ModuleLanguage on every row, matching the Language on
// the Module rows above. Module identity is (name, lang), so the edge statement
// resolves its target on both properties; a row that named the module but not
// its language would match no node and the edge would simply not be written.
//
// The rows also carry the #7345 import flags. Generation 1 sets type_only on the
// express edge and deferred plus inferred on the fmt edge; generation 2 flips
// every one of them, so the proof covers both directions: a true overwritten by
// false (the stale-flag case) and a false overwritten by true.
func importEdgeRows(generation int) []canonical.ImportRow {
	app := canonical.ImportRow{FilePath: importEdgeRepoPath + "/src/app.ts", ModuleName: "5691-test-express", ModuleLanguage: "typescript", ImportedName: "Router", Alias: "R", LineNumber: 2}
	fmtRow := canonical.ImportRow{FilePath: importEdgeRepoPath + "/src/main.go", ModuleName: "5691-test-fmt", ModuleLanguage: "go", ImportedName: "", LineNumber: 4}
	if generation == 1 {
		app.TypeOnly = true
		fmtRow.Deferred = true
		fmtRow.Inferred = true
	} else {
		fmtRow.TypeOnly = true
	}
	return []canonical.ImportRow{app, fmtRow}
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

	if err := writer.Write(ctx, importEdgeMaterialization("gen1", true, importEdgeRows(1))); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	assertImportEdgeTruth(ctx, t, exec, "gen1", [2][3]bool{{true, false, false}, {false, true, true}})

	// A second generation re-projects the same edges with every flag flipped.
	// Every edge must be re-MERGEd onto itself, never duplicated and never
	// dropped, and each flag must take the new value: a stale true surviving the
	// re-projection would leave an import excluded from cycles it belongs to.
	if err := writer.Write(ctx, importEdgeMaterialization("gen2", false, importEdgeRows(2))); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	assertImportEdgeTruth(ctx, t, exec, "gen2 re-projection", [2][3]bool{{false, false, false}, {true, false, false}})
}

// assertImportEdgeTruth reads the projected IMPORTS edges back and checks the
// full expected set, not just a count: a count alone would pass if two edges
// existed with the wrong imported_name on each.
//
// wantFlags is [type_only, deferred, inferred] for each edge in file order
// (src/app.ts, then src/main.go). A flag property that is absent or not a
// boolean fails the test rather than reading as false, because an edge that was
// never written the flags is the case a reader must not mistake for a runtime
// edge.
func assertImportEdgeTruth(ctx context.Context, t *testing.T, exec liveExecutor, label string, wantFlags [2][3]bool) {
	t.Helper()

	rows, err := exec.Run(ctx, `MATCH (f:File)-[r:IMPORTS]->(m:Module)
WHERE f.repo_id = $repo_id
RETURN f.relative_path AS file, m.name AS module, r.imported_name AS imported_name, r.alias AS alias, r.line_number AS line_number,
       r.type_only AS type_only, r.deferred AS deferred, r.inferred AS inferred`,
		map[string]any{"repo_id": importEdgeRepoID})
	if err != nil {
		t.Fatalf("%s: read IMPORTS edges: %v", label, err)
	}

	type edge struct {
		file, module, imported, alias string
		line                          int64
		flags                         [3]bool
	}
	got := make([]edge, 0, len(rows))
	for _, row := range rows {
		file, _ := row["file"].(string)
		module, _ := row["module"].(string)
		imported, _ := row["imported_name"].(string)
		alias, _ := row["alias"].(string)
		line, _ := row["line_number"].(int64)
		var flags [3]bool
		for i, key := range []string{"type_only", "deferred", "inferred"} {
			value, ok := row[key].(bool)
			if !ok {
				t.Fatalf("%s: edge %s->%s property %q = %#v, want an explicit boolean", label, file, module, key, row[key])
			}
			flags[i] = value
		}
		got = append(got, edge{file, module, imported, alias, line, flags})
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].file != got[j].file {
			return got[i].file < got[j].file
		}
		return got[i].module < got[j].module
	})

	want := []edge{
		{"src/app.ts", "5691-test-express", "Router", "R", 2, wantFlags[0]},
		{"src/main.go", "5691-test-fmt", "", "", 4, wantFlags[1]},
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
