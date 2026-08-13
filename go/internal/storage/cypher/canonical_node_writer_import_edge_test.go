// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// This file holds the canonical node writer's import-edge identity check and
// the shared materialization it drives, split out of canonical_node_writer_test.go
// to stay under the repo's 500-line file cap.

// phaseOrderMaterialization is the hand-built materialization the phase-order
// test writes, factored out so the emitted-statement guard below can drive the
// same fixture rather than a second copy that could drift from it.
func phaseOrderMaterialization() projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
			RemoteURL: "https://github.com/org/my-repo",
			RepoSlug:  "org/my-repo",
			HasRemote: true,
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 5, EndLine: 10, Language: "go", RepoID: "repo-1"},
		},
		Modules: []projector.ModuleRow{
			{Name: "fmt", Language: "go"},
		},
		Imports: []projector.ImportRow{
			{FilePath: "/repos/my-repo/src/main.go", ModuleName: "fmt", ModuleLanguage: "go", ImportedName: "fmt", LineNumber: 3},
		},
	}
}

// TestCanonicalNodeWriterImportEdgeRowsResolveToAnEmittedModule closes the gap
// its neighbour TestCanonicalNodeWriterWritePhaseOrder cannot cover. That test
// drives a fake executor and asserts only the ORDER of the emitted phases, so a
// materialization whose ImportRow carries no ModuleLanguage still produces an
// IMPORTS statement -- with module_language "" -- and the phase order is
// unchanged. It passes, and the edge would never land.
//
// This reads the emitted parameters instead: every IMPORTS row's
// (module_name, module_language) must appear among the rows the Module phase
// MERGEs, because the writer resolves the edge target with
// `MATCH (m:Module {name: row.module_name, lang: row.module_language})`. A
// MATCH that finds nothing yields no row, so the MERGE never runs and nothing
// errors or logs.
//
// It works on the statements rather than the materialization, so it also
// catches the writer itself dropping the language while building parameters --
// one step closer to the backend than the sibling checks in
// internal/projector and internal/replay/offlinetier.
func TestCanonicalNodeWriterImportEdgeRowsResolveToAnEmittedModule(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	if err := writer.Write(context.Background(), phaseOrderMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	type moduleKey struct{ name, lang string }
	merged := map[moduleKey]struct{}{}
	imported := make([]map[string]any, 0, 1)
	for _, call := range exec.calls {
		rows, _ := call.Parameters["rows"].([]map[string]any)
		switch {
		case strings.Contains(call.Cypher, "MERGE (m:Module"):
			for _, row := range rows {
				name, _ := row["name"].(string)
				lang, _ := row["language"].(string)
				merged[moduleKey{name: name, lang: lang}] = struct{}{}
			}
		case strings.Contains(call.Cypher, "[r:IMPORTS]->"):
			imported = append(imported, rows...)
		}
	}

	if len(merged) == 0 || len(imported) == 0 {
		t.Fatalf("emitted %d Module rows and %d IMPORTS rows; this check needs both to prove anything",
			len(merged), len(imported))
	}
	for _, row := range imported {
		name, _ := row["module_name"].(string)
		lang, _ := row["module_language"].(string)
		if _, ok := merged[moduleKey{name: name, lang: lang}]; !ok {
			t.Fatalf("IMPORTS row %+v targets Module{name=%q, lang=%q}, which the Module phase never MERGEd; "+
				"the MATCH would bind no node and the edge would be dropped without an error", row, name, lang)
		}
	}
}
