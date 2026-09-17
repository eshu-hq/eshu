// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestDeltaFileAndDirectoryRetractsUsePositiveInWorklist is the #6715
// regression. The delta file/directory node DELETE statements must seed their
// worklist with a positive `IN $file_paths` / `IN $directory_paths`
// predicate, not an `UNWIND ... AS ...` seed followed by a compound WHERE.
//
// Measured on the pinned NornicDB v1.3.3 backend at 600k-node scale, the
// UNWIND-seeded compound shape costs 87-216s per execution while the
// equivalent IN shape costs 0.1-9s with identical deleted row sets
// (remote proof, reported on #6715). The phase-group chunker splits both
// shapes at the same batch size, so nothing about batching is lost.
//
// Statements seeded by `UNWIND $rows AS row` (directory parent-edge and
// entity-containment refreshes, keyed by uid rather than path) are out of
// scope here: they carry a distinct per-row-scan pathology that needs a
// two-trip redesign, tracked separately.
func TestDeltaFileAndDirectoryRetractsUsePositiveInWorklist(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:         "scope-1",
		GenerationID:    "gen-2",
		RepoID:          "repo-1",
		RepoPath:        "/repos/repo",
		DeltaProjection: true,
		DeltaFilePaths:  []string{"/repos/repo/changed.go"},
		DeltaDeletedFilePaths: []string{
			"/repos/repo/old/deleted.go",
		},
		DeltaDeletedDirectoryPaths: []string{
			"/repos/repo/old/emptydir",
		},
		Files: []projector.FileRow{
			{Path: "/repos/repo/changed.go", RepoID: "repo-1"},
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/repo/old/emptydir", ParentPath: "/repos/repo/old", RepoID: "repo-1", Depth: 2},
		},
	}

	type want struct {
		// match identifies the statement under test.
		contains string
		// worklist is the positive IN token the statement must carry.
		worklist string
	}
	wants := []want{
		{contains: "DETACH DELETE f", worklist: "IN $file_paths"},
		{contains: "MATCH (d)-[r:CONTAINS]-()", worklist: "IN $directory_paths"},
	}
	saw := make(map[string]int)
	for _, stmt := range writer.buildDeltaRetractStatements(mat) {
		for _, w := range wants {
			if !strings.Contains(stmt.Cypher, w.contains) {
				continue
			}
			saw[w.contains]++
			if strings.Contains(stmt.Cypher, "UNWIND $file_paths AS") ||
				strings.Contains(stmt.Cypher, "UNWIND $directory_paths AS") {
				t.Errorf("retract %q uses UNWIND-seeded worklist (issue #6715):\n%s", w.contains, stmt.Cypher)
			}
			if !strings.Contains(stmt.Cypher, w.worklist) {
				t.Errorf("retract %q = %q, want positive %q worklist", w.contains, stmt.Cypher, w.worklist)
			}
		}
		// Directory node deletes (deleted + empty) share DETACH DELETE d.
		if strings.Contains(stmt.Cypher, "DETACH DELETE d") {
			saw["DETACH DELETE d"]++
			if strings.Contains(stmt.Cypher, "UNWIND $directory_paths AS") {
				t.Errorf("directory retract uses UNWIND-seeded worklist (issue #6715):\n%s", stmt.Cypher)
			}
			if !strings.Contains(stmt.Cypher, "IN $directory_paths") {
				t.Errorf("directory retract = %q, want positive IN $directory_paths worklist", stmt.Cypher)
			}
		}
	}
	for _, w := range wants {
		if saw[w.contains] == 0 {
			t.Errorf("no built retract statement contains %q", w.contains)
		}
	}
	if saw["DETACH DELETE d"] < 2 {
		t.Errorf("DETACH DELETE d statements = %d, want >= 2 (deleted + empty directories)", saw["DETACH DELETE d"])
	}
}
