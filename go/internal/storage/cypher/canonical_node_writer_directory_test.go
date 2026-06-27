// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestCanonicalNodeWriterDirectoryDepthOrder proves directory writes are split
// into a node phase (MERGE by path, no parent MATCH) that commits before the
// edge phase (parent CONTAINS, MATCH'd endpoints). This ordering is required on
// NornicDB, where a node MERGE'd earlier in the same transaction is not visible
// to a later UNWIND-driven MATCH — without it, a depth-N directory whose parent
// was MERGE'd in the same batch would fail its parent MATCH and silently never
// be created, dropping every file and entity nested beneath it.
func TestCanonicalNodeWriterDirectoryDepthOrder(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "my-repo",
			Path:   "/repos/my-repo",
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/src/pkg/sub", Name: "sub", ParentPath: "/repos/my-repo/src/pkg", RepoID: "repo-1", Depth: 2},
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
			{Path: "/repos/my-repo/src/pkg", Name: "pkg", ParentPath: "/repos/my-repo/src", RepoID: "repo-1", Depth: 1},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Collect directory-phase calls (node MERGEs and parent-edge writes both
	// reference `Directory {path: row.path}`).
	nodeCallIdx, firstEdgeCallIdx := -1, -1
	var edgeCalls []Statement
	for i, call := range exec.calls {
		if call.Operation != OperationCanonicalUpsert ||
			!strings.Contains(call.Cypher, "Directory {path: row.path}") {
			continue
		}
		isEdge := strings.Contains(call.Cypher, "-[rel:CONTAINS]->")
		if isEdge {
			if firstEdgeCallIdx == -1 {
				firstEdgeCallIdx = i
			}
			edgeCalls = append(edgeCalls, call)
			continue
		}
		// Node-phase statement: a pure MERGE by path with no parent MATCH, so it
		// carries no cross-row visibility dependency.
		if strings.Contains(call.Cypher, "MATCH ") {
			t.Fatalf("directory node statement must not MATCH a parent, got: %s", call.Cypher)
		}
		nodeCallIdx = i
	}

	if nodeCallIdx == -1 {
		t.Fatal("expected a directory node-phase MERGE, got none")
	}
	if firstEdgeCallIdx == -1 {
		t.Fatal("expected directory parent-edge writes, got none")
	}
	// The fix: every directory node MERGE must commit before any parent-edge
	// MATCH runs, so depth-N edges can see their already-created parents.
	if nodeCallIdx >= firstEdgeCallIdx {
		t.Fatalf("directory node phase (idx %d) must precede directory edge phase (idx %d)", nodeCallIdx, firstEdgeCallIdx)
	}
	// Edge writes: depth-0 wires the Repository parent, depth-N wires the parent
	// Directory. Both endpoints are MATCH'd (never MERGE'd) so they must exist.
	var sawRepoEdge, sawDirEdge bool
	for _, call := range edgeCalls {
		if strings.Contains(call.Cypher, "MATCH (r:Repository") {
			sawRepoEdge = true
		}
		if strings.Contains(call.Cypher, "MATCH (p:Directory") {
			sawDirEdge = true
		}
	}
	if !sawRepoEdge {
		t.Fatal("expected a depth-0 directory edge matching the Repository parent")
	}
	if !sawDirEdge {
		t.Fatal("expected a depth-N directory edge matching the parent Directory")
	}
}

// TestDirectoryEdgesStayInlineOnAtomicPath proves the directory parent-edge phase
// is NOT deferred out of the main atomic group (unlike the package_registry
// edges). Its endpoints are single-label Directory nodes MERGE'd in the
// directories phase, and NornicDB provides cross-statement read-your-writes for
// single-label nodes within one atomic group (the RequireAtomicGroup "file entity
// containment" conformance case), so the edge MATCH resolves inline. Deferring it
// would be inconsistent with the inline File -> Directory CONTAINS edge.
func TestDirectoryEdgesStayInlineOnAtomicPath(t *testing.T) {
	t.Parallel()

	phases := []canonicalWritePhase{
		{name: CanonicalPhaseDirectories, statements: []Statement{{Cypher: "dir-node"}}},
		{name: CanonicalPhaseDirectoryEdges, statements: []Statement{{Cypher: "dir-edge"}}},
		{name: CanonicalPhaseFiles, statements: []Statement{{Cypher: "file-node"}}},
		{name: canonicalPhasePackageRegistryVersionEdges, statements: []Statement{{Cypher: "pkg-edge"}}},
	}

	main, deferred := partitionDeferredPackageRegistryEdgePhases(phases)

	has := func(stmts []Statement, cy string) bool {
		for _, s := range stmts {
			if s.Cypher == cy {
				return true
			}
		}
		return false
	}

	// Only the multi-label package_registry edges defer.
	if !has(deferred, "pkg-edge") {
		t.Fatal("package_registry edges must be deferred to the second ExecuteGroup")
	}
	if has(deferred, "dir-edge") {
		t.Fatal("directory_edges must stay inline (single-label read-your-writes), not deferred")
	}
	// directory nodes, directory edges, and files all run in the main group.
	for _, cy := range []string{"dir-node", "dir-edge", "file-node"} {
		if !has(main, cy) {
			t.Fatalf("%q must run in the main atomic group", cy)
		}
	}
}
