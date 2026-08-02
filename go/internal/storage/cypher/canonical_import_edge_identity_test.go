// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestCanonicalImportEdgeMergeKeyIncludesImportedName pins the IMPORTS edge
// identity to (file, module, imported_name).
//
// Issue #5691 gave these edges their first producer, and that producer emits
// one row per imported symbol: `import { Router, json } from "express"` is two
// rows against the same file and module. A MERGE keyed on the endpoints alone
// folds both into one edge, and the second row's SET silently overwrites the
// first — so /code/import-dependencies would report exactly one of the two
// symbols, with which one it reports decided by batch ordering.
func TestCanonicalImportEdgeMergeKeyIncludesImportedName(t *testing.T) {
	t.Parallel()

	if !strings.Contains(canonicalNodeImportEdgeCypher, "MERGE (f)-[r:IMPORTS {imported_name: row.imported_name}]->(m)") {
		t.Fatalf("IMPORTS MERGE is not keyed on imported_name:\n%s", canonicalNodeImportEdgeCypher)
	}
	// The property is part of the identity now, so re-SETting it on match
	// would be redundant at best and a no-op rewrite of the merge key at worst.
	if strings.Contains(canonicalNodeImportEdgeCypher, "r.imported_name = row.imported_name") {
		t.Fatalf("imported_name is both the MERGE key and a SET target:\n%s", canonicalNodeImportEdgeCypher)
	}
}

// TestCanonicalNodeWriterEmitsOneImportRowPerSymbol proves the writer carries
// every extracted symbol through to a distinct parameter row rather than
// deduplicating them on the way to the backend.
func TestCanonicalNodeWriterEmitsOneImportRowPerSymbol(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/src/app.ts", RelativePath: "src/app.ts", Name: "app.ts", Language: "typescript", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
		Modules: []projector.ModuleRow{
			{Name: "express", Language: "typescript"},
		},
		Imports: []projector.ImportRow{
			{FilePath: "/repos/my-repo/src/app.ts", ModuleName: "express", ImportedName: "Router", LineNumber: 2},
			{FilePath: "/repos/my-repo/src/app.ts", ModuleName: "express", ImportedName: "json", LineNumber: 2},
		},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	importedNames := map[string]bool{}
	for _, call := range exec.calls {
		if !strings.Contains(call.Cypher, "[r:IMPORTS") {
			continue
		}
		rows, ok := call.Parameters["rows"].([]map[string]any)
		if !ok {
			continue
		}
		for _, row := range rows {
			name, _ := row["imported_name"].(string)
			importedNames[name] = true
		}
	}

	for _, want := range []string{"Router", "json"} {
		if !importedNames[want] {
			t.Errorf("imported_name %q never reached the writer; got %v", want, importedNames)
		}
	}
}
