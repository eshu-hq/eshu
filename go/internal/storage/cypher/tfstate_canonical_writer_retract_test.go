// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestTerraformStateResourceRetractStatementsSkipsUnderDeltaProjection
// proves the P0 guard: terraformStateResourceRetractStatements must not
// build an unscoped, scope-wide DETACH DELETE when the materialization is a
// file-scoped delta cycle. mat.TerraformStateResources is populated only
// from terraform_state envelopes present in THIS materialization's input
// (internal/projector/tfstate_canonical.go's extractTerraformStateRows), so
// a delta cycle triggered by an unrelated file edit carries none. Every
// sibling retraction in this package already guards DeltaProjection
// (buildRetractStatements and buildEntityRetractStatements delegate to a
// delta-scoped variant; buildRepositoryCleanupStatements skips outright).
// Before this fix, terraformStateResourceRetractStatements guarded only
// mat.FirstGeneration, so an unrelated-file delta cycle would DETACH DELETE
// the entire existing TerraformStateResource/TerraformResource population
// for this scope while the upsert phase has zero rows to recreate it --
// silent, permanent, full-population data loss. This test must fail against
// pre-fix HEAD (it returns 2 statements, not 0) and pass once the guard
// lands.
func TestTerraformStateResourceRetractStatementsSkipsUnderDeltaProjection(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:                 "tf-scope-delta",
		GenerationID:            "tf-generation-delta",
		FirstGeneration:         false,
		DeltaProjection:         true,
		TerraformStateResources: nil, // unrelated-file delta: no tfstate envelope in this batch
	}

	statements := writer.terraformStateResourceRetractStatements(mat)
	if len(statements) != 0 {
		t.Fatalf(
			"terraformStateResourceRetractStatements() under DeltaProjection with no batch resources = %d statements, want 0 (an unscoped retract here would DETACH DELETE the entire scope population with nothing to recreate it): %#v",
			len(statements), statements,
		)
	}
}

// TestTerraformStateResourceRetractStatementsRunsOnNonDeltaGeneration proves
// the guard is scoped to delta cycles only: a normal (non-delta,
// non-first-generation) cycle must still emit the two generation-gated
// retraction statements. Guards against an over-broad fix that disables
// retraction entirely.
func TestTerraformStateResourceRetractStatementsRunsOnNonDeltaGeneration(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:         "tf-scope-full",
		GenerationID:    "tf-generation-full",
		FirstGeneration: false,
		DeltaProjection: false,
	}

	statements := writer.terraformStateResourceRetractStatements(mat)
	if got, want := len(statements), 2; got != want {
		t.Fatalf("terraformStateResourceRetractStatements() on a non-delta generation = %d statements, want %d", got, want)
	}
}
