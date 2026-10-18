// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// The attributed retirement shape (OPTIONAL MATCH + WITH collect + RETURN
// dropped) is Neo4j-only: NornicDB routes multi-clause statements into an
// interpreter that drops the WHERE guard and deletes the live node in steady
// state (live probe: 1 node + 1 relationship deleted, RETURN yields the
// literal "dropped"). Writers targeting NornicDB must emit the bare shape,
// which both backends execute correctly.

func bareRetirementMaterialization() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:      "scope:bare",
		GenerationID: "gen-2",
		RepoID:       "repository:bare",
		RepoPath:     "/bare",
		Repository: &canonical.RepositoryRow{
			RepoID: "repository:bare", Name: "bare", Path: "/bare",
			LocalPath: "/bare",
		},
	}
}

// The default writer keeps the attributed shape: the #5385 T4 cascade
// deletes first, then the path conflict with its pre-delete collection
// and RETURN dropped.
func TestCanonicalNodeWriterDefaultRetirementIsAttributed(t *testing.T) {
	cleanup := NewCanonicalNodeWriter(nil, 500, nil).buildRepositoryCleanupStatements(bareRetirementMaterialization())
	if len(cleanup) != 4 {
		t.Fatalf("cleanup statements = %d, want 4 (3 cascades + path conflict)", len(cleanup))
	}
	if !IsRepositoryPathCleanupCypher(cleanup[3].Cypher) {
		t.Fatalf("default cleanup tail is not the attributed shape:\n%s", cleanup[3].Cypher)
	}
}

// The bare opt-out emits the pre-split single-clause shape with identical
// parameters and summary key, so the report pipeline still attributes the
// backend summary counters (attribution=false) instead of going silent.
func TestCanonicalNodeWriterBareRetirementEmitsBareShape(t *testing.T) {
	mat := bareRetirementMaterialization()
	wantParams := map[string]any{
		"repo_id":                   "repository:bare",
		"path":                      "/bare",
		StatementMetadataSummaryKey: "repository_cleanup lookup=path_conflict",
	}
	bare := NewCanonicalNodeWriter(nil, 500, nil).WithBareRepositoryRetirement().
		buildRepositoryCleanupStatements(mat)
	if len(bare) != 4 {
		t.Fatalf("bare cleanup statements = %d, want 4 (3 cascades + path conflict)", len(bare))
	}
	if IsRepositoryPathCleanupCypher(bare[3].Cypher) {
		t.Fatalf("bare cleanup tail is the attributed shape, want the bare shape")
	}
	if strings.Contains(bare[3].Cypher, "OPTIONAL MATCH") || strings.Contains(bare[3].Cypher, "RETURN dropped") {
		t.Fatalf("bare cleanup tail carries the NornicDB-unsafe clauses:\n%s", bare[3].Cypher)
	}
	for key, want := range wantParams {
		if bare[3].Parameters[key] != want {
			t.Fatalf("bare cleanup param %s = %#v, want %#v", key, bare[3].Parameters[key], want)
		}
	}
	collecting := NewCanonicalNodeWriter(nil, 500, nil).buildRepositoryCleanupStatements(mat)
	if len(collecting) != 4 {
		t.Fatalf("collecting cleanup statements = %d, want 4 (3 cascades + path conflict)", len(collecting))
	}
	for key, want := range wantParams {
		if collecting[3].Parameters[key] != want {
			t.Fatalf("collecting cleanup param %s = %#v, want %#v", key, collecting[3].Parameters[key], want)
		}
	}
}

// The retirement capture recognizes the bare shape, so a NornicDB writer's
// committed bare retirement still reaches the report (attribution=false,
// outcome counter moves) instead of going silent.
func TestCaptureRepositoryRetirementRecognizesBareShape(t *testing.T) {
	mat := bareRetirementMaterialization()
	stmts := NewCanonicalNodeWriter(nil, 500, nil).WithBareRepositoryRetirement().
		buildRepositoryCleanupStatements(mat)
	ctx, collector := captureRepositoryRetirement(context.Background(), stmts)
	if collector == nil {
		t.Fatal("captureRepositoryRetirement(bare cleanup) = nil collector, want capture")
	}
	_ = ctx
}
