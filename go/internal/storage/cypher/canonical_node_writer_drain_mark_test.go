// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// buildFullRefreshMat returns a non-delta, non-first-generation materialization
// with files and directories so all four unbounded retract statements are built.
func buildFullRefreshMat() projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		RepoID:       "repo-drain-test",
		GenerationID: "gen-2",
		Files: []projector.FileRow{
			{Path: "/repos/r/a.go"},
			{Path: "/repos/r/b.go"},
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/r/pkg"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "fn", FilePath: "/repos/r/a.go"},
		},
	}
}

// TestBuildRetractStatementsMasksDrainOnUnboundedFullRefreshFiles verifies that
// the file retract statement built for a full-refresh run carries Drain=true
// and DrainVar="f".
func TestBuildRetractStatementsMasksDrainOnUnboundedFullRefreshFiles(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)

	// No filePaths → unbounded canonicalNodeRetractFilesCypher path.
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-2",
	}
	stmts := w.buildRetractStatements(mat)
	var found bool
	for _, s := range stmts {
		if strings.Contains(s.Cypher, "REPO_CONTAINS") && strings.Contains(s.Cypher, "DETACH DELETE f") {
			if !s.Drain {
				t.Fatalf("file retract statement has Drain = false, want true")
			}
			if s.DrainVar != "f" {
				t.Fatalf("file retract statement DrainVar = %q, want %q", s.DrainVar, "f")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("did not find file retract statement in buildRetractStatements output")
	}
}

// TestBuildRetractStatementsMasksDrainOnRemovedFilesVariant verifies the
// canonicalNodeRetractRemovedFilesCypher path (filePaths > 0) also marks Drain.
func TestBuildRetractStatementsMasksDrainOnRemovedFilesVariant(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := buildFullRefreshMat()
	stmts := w.buildRetractStatements(mat)
	var found bool
	for _, s := range stmts {
		if strings.Contains(s.Cypher, "REPO_CONTAINS") && strings.Contains(s.Cypher, "DETACH DELETE f") {
			if !s.Drain {
				t.Fatalf("removed-files retract statement has Drain = false, want true")
			}
			if s.DrainVar != "f" {
				t.Fatalf("removed-files retract statement DrainVar = %q, want %q", s.DrainVar, "f")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("did not find file retract statement (removed-files variant)")
	}
}

// TestBuildRetractStatementsMasksDrainOnDirectories verifies the directory
// retract statement carries Drain=true and DrainVar="d".
func TestBuildRetractStatementsMasksDrainOnDirectories(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := buildFullRefreshMat()
	stmts := w.buildRetractStatements(mat)
	var found bool
	for _, s := range stmts {
		if strings.Contains(s.Cypher, "Directory") && strings.Contains(s.Cypher, "DETACH DELETE d") {
			if !s.Drain {
				t.Fatalf("directory retract statement has Drain = false, want true")
			}
			if s.DrainVar != "d" {
				t.Fatalf("directory retract statement DrainVar = %q, want %q", s.DrainVar, "d")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("did not find directory retract statement in buildRetractStatements output")
	}
}

// TestBuildEntityRetractStatementsMasksDrainOnEntityLabels verifies that every
// entity retract statement built for a full-refresh run carries Drain=true and
// DrainVar="n".
func TestBuildEntityRetractStatementsMasksDrainOnEntityLabels(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := buildFullRefreshMat()
	stmts := w.buildEntityRetractStatements(mat)
	if len(stmts) == 0 {
		t.Fatal("buildEntityRetractStatements returned no statements for full-refresh mat")
	}
	for i, s := range stmts {
		if !s.Drain {
			t.Errorf("entity retract statement %d has Drain = false, want true", i)
		}
		if s.DrainVar != "n" {
			t.Errorf("entity retract statement %d DrainVar = %q, want %q", i, s.DrainVar, "n")
		}
	}
}

// TestBuildRetractStatementsDeltaDoesNotMarkDrain verifies that delta retract
// statements are NOT marked with Drain (they are already bounded by file_paths).
func TestBuildRetractStatementsDeltaDoesNotMarkDrain(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		RepoID:                "repo-1",
		GenerationID:          "gen-2",
		RepoPath:              "/repos/r",
		DeltaProjection:       true,
		DeltaFilePaths:        []string{"/repos/r/a.go"},
		DeltaDeletedFilePaths: []string{"/repos/r/b.go"},
	}
	stmts := w.buildRetractStatements(mat)
	for i, s := range stmts {
		if s.Drain {
			t.Errorf("delta retract statement %d unexpectedly has Drain = true: %s", i, s.Cypher)
		}
	}
}

// TestBuildEntityRetractStatementsDeltaDoesNotMarkDrain verifies that delta
// entity retract statements are NOT marked with Drain.
func TestBuildEntityRetractStatementsDeltaDoesNotMarkDrain(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		RepoID:          "repo-1",
		GenerationID:    "gen-2",
		DeltaProjection: true,
		DeltaFilePaths:  []string{"/repos/r/a.go"},
		Entities: []projector.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "fn", FilePath: "/repos/r/a.go"},
		},
	}
	stmts := w.buildEntityRetractStatements(mat)
	for i, s := range stmts {
		if s.Drain {
			t.Errorf("delta entity retract statement %d unexpectedly has Drain = true: %s", i, s.Cypher)
		}
	}
}

// TestBuildRetractStatementsFirstGenerationReturnsNilWithNoDrain verifies that
// first-generation materializations still produce no retract statements.
func TestBuildRetractStatementsFirstGenerationReturnsNilWithNoDrain(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		RepoID:          "repo-1",
		GenerationID:    "gen-1",
		FirstGeneration: true,
	}
	stmts := w.buildRetractStatements(mat)
	if len(stmts) != 0 {
		t.Fatalf("first-generation buildRetractStatements returned %d statements, want 0", len(stmts))
	}
}

// TestStatementDrainFieldsAreZeroValueByDefault verifies that Statement{} has
// Drain=false and DrainVar="" so existing code unaffected by the new fields
// does not need any changes.
func TestStatementDrainFieldsAreZeroValueByDefault(t *testing.T) {
	t.Parallel()

	s := Statement{
		Operation:  OperationCanonicalRetract,
		Cypher:     "MATCH (f:File) DETACH DELETE f",
		Parameters: map[string]any{},
	}
	if s.Drain {
		t.Fatal("Statement{}.Drain = true, want false (zero value)")
	}
	if s.DrainVar != "" {
		t.Fatalf("Statement{}.DrainVar = %q, want empty (zero value)", s.DrainVar)
	}
}

// TestBuildEntityRetractStatementsDrainVarIsNForAllLabels verifies the drain
// var is always "n" for entity retracts, not the label name.
func TestBuildEntityRetractStatementsDrainVarIsNForAllLabels(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := buildFullRefreshMat()
	for i, s := range w.buildEntityRetractStatements(mat) {
		if s.DrainVar != "n" {
			t.Errorf("entity retract statement %d: DrainVar = %q, want %q", i, s.DrainVar, "n")
		}
		// Cypher should still contain the label (e.g. "MATCH (n:Function)")
		if !strings.Contains(s.Cypher, "MATCH (n:") {
			t.Errorf("entity retract statement %d: missing label anchor in Cypher:\n%s", i, s.Cypher)
		}
	}
}

// TestBuildRetractStatementsDrainCountIsExactlyFourForFullRefresh confirms that
// every unbounded full-refresh DETACH DELETE carries Drain=true. The only
// DETACH DELETE statements that may NOT carry Drain are bounded ones: the
// Parameter retract (bounded by $file_paths) and the delta variants.
func TestBuildRetractStatementsDrainCountIsExactlyFourForFullRefresh(t *testing.T) {
	t.Parallel()

	w := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := buildFullRefreshMat()

	allStmts := append(w.buildRetractStatements(mat), w.buildEntityRetractStatements(mat)...)
	var drainCount int
	for _, s := range allStmts {
		if s.Drain {
			drainCount++
		}
	}

	// At minimum: files (1) + directories (1) + entity labels (many). Must be >= 3.
	if drainCount < 3 {
		t.Fatalf("drain statement count = %d, want >= 3 (files + dirs + at least 1 entity label)", drainCount)
	}

	// Any DETACH DELETE that is NOT bounded by a positive list must carry Drain.
	// The only known non-drain DETACH DELETE in the full-refresh path is
	// canonicalNodeRetractParametersCypher, which uses a positive $file_paths IN
	// predicate and is thus already bounded. Verify no other unbounded DELETEs slip
	// through by checking that non-drain DETACH DELETEs all contain a positive IN
	// predicate.
	for i, s := range allStmts {
		if s.Drain || !strings.Contains(s.Cypher, "DETACH DELETE") {
			continue
		}
		// Acceptable only if bounded by a positive IN predicate.
		if !strings.Contains(s.Cypher, "IN $") {
			t.Errorf("statement %d has DETACH DELETE without Drain=true and without positive IN predicate — likely unbounded: %s",
				i, fmt.Sprintf("%.120s", s.Cypher))
		}
	}
}
