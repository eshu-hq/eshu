// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestTerraformStateMatchesConfigEdgeRetractStatementsSkipsOnFirstGeneration
// proves the guard mirroring terraformStateResourceRetractStatements' own
// FirstGeneration skip: a scope's first generation never wrote a MATCHES_STATE
// edge before, so nothing can be stale yet.
func TestTerraformStateMatchesConfigEdgeRetractStatementsSkipsOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:         "tf-scope-p1a",
		GenerationID:    "tf-generation-p1a-1",
		FirstGeneration: true,
	}

	statements := writer.terraformStateMatchesConfigEdgeRetractStatements(mat)
	if len(statements) != 0 {
		t.Fatalf("terraformStateMatchesConfigEdgeRetractStatements() on FirstGeneration = %d statements, want 0: %#v", len(statements), statements)
	}
}

// TestTerraformStateMatchesConfigEdgeRetractStatementsSkipsUnderDeltaProjection
// proves the guard mirroring terraformStateResourceRetractStatements' own
// DeltaProjection skip: mat.TerraformStateResources is populated only from
// terraform_state envelopes present in THIS materialization's input, so a
// delta cycle triggered by an unrelated file edit carries none, and an
// unscoped generation-gated edge retract would delete every MATCHES_STATE
// edge for state resources this delta cycle never touched.
func TestTerraformStateMatchesConfigEdgeRetractStatementsSkipsUnderDeltaProjection(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:         "tf-scope-p1a",
		GenerationID:    "tf-generation-p1a-2",
		FirstGeneration: false,
		DeltaProjection: true,
	}

	statements := writer.terraformStateMatchesConfigEdgeRetractStatements(mat)
	if len(statements) != 0 {
		t.Fatalf("terraformStateMatchesConfigEdgeRetractStatements() under DeltaProjection = %d statements, want 0: %#v", len(statements), statements)
	}
}

// TestTerraformStateMatchesConfigEdgeRetractStatementsRunsOnNonDeltaGeneration
// proves the guard is scoped to delta cycles only: a normal (non-delta,
// non-first-generation) cycle must still emit the generation-gated retraction
// statement, with the correct scope/generation binding, the
// TerraformStateResource anchor, the MATCHES_STATE relationship, the
// evidence_source guard, and Drain=true (a relationship DELETE mixed into the
// terraform_state phase's grouped statements needs the same NornicDB
// standalone-autocommit treatment the Atlantis/Flux/GitLab/Helm structural
// edge retracts already use -- see canonical_atlantis_edges.go).
func TestTerraformStateMatchesConfigEdgeRetractStatementsRunsOnNonDeltaGeneration(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:         "tf-scope-p1a",
		GenerationID:    "tf-generation-p1a-3",
		FirstGeneration: false,
		DeltaProjection: false,
	}

	statements := writer.terraformStateMatchesConfigEdgeRetractStatements(mat)
	if got, want := len(statements), 1; got != want {
		t.Fatalf("terraformStateMatchesConfigEdgeRetractStatements() on a non-delta generation = %d statements, want %d: %#v", got, want, statements)
	}

	stmt := statements[0]
	if !strings.Contains(stmt.Cypher, "MATCH (s:TerraformStateResource)") {
		t.Fatalf("Cypher = %q, want a TerraformStateResource anchor", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCHES_STATE") {
		t.Fatalf("Cypher = %q, want a MATCHES_STATE relationship", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "e.evidence_source = 'projector/tfstate'") {
		t.Fatalf("Cypher = %q, want the evidence_source guard so only this writer's own edges are ever retracted", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "s.generation_id = $generation_id") {
		t.Fatalf("Cypher = %q, want the anchor scoped to state resources refreshed THIS generation", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "e.generation_id <> $generation_id") {
		t.Fatalf("Cypher = %q, want the generation-gated staleness predicate on the edge", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "DELETE e") || strings.Contains(stmt.Cypher, "DETACH DELETE") {
		t.Fatalf("Cypher = %q, want a bare relationship DELETE, not DETACH DELETE (this must never delete a node)", stmt.Cypher)
	}
	if !stmt.Drain {
		t.Fatalf("Drain = false, want true: this relationship DELETE is mixed into the terraform_state phase's grouped statements (#4476 class)")
	}
	if stmt.DrainVar != "" {
		t.Fatalf("DrainVar = %q, want empty (bounded mixed-phase relationship retract, not an unbounded node drain loop)", stmt.DrainVar)
	}
	if got, want := stmt.Parameters["scope_id"], "tf-scope-p1a"; got != want {
		t.Fatalf("scope_id = %#v, want %q", got, want)
	}
	if got, want := stmt.Parameters["generation_id"], "tf-generation-p1a-3"; got != want {
		t.Fatalf("generation_id = %#v, want %q", got, want)
	}
}

// TestBuildTerraformStateStatementsRetractsEdgeBeforeMerge proves
// buildTerraformStateStatements wires the new edge retract into the phase
// ahead of the MATCHES_STATE MERGE write, mirroring
// canonical_atlantis_edges.go's retract-before-MERGE ordering (a relationship
// DELETE emitted after a MERGE that just rewrote the current edge would have
// nothing stale left to find, but the ordering also keeps the Drain-marked
// retract as its own standalone autocommit statement ahead of the grouped
// MERGE, matching the established precedent exactly).
func TestBuildTerraformStateStatementsRetractsEdgeBeforeMerge(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil).
		WithTerraformStateConfigMatchResolver(fakeConfigMatchResolver{})
	mat := projector.CanonicalMaterialization{
		ScopeID:         "tf-scope-p1a-order",
		GenerationID:    "tf-generation-p1a-order",
		FirstGeneration: false,
		DeltaProjection: false,
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:              "tf-resource-p1a-order",
			Address:          "aws_instance.web",
			Mode:             "managed",
			ResourceType:     "aws_instance",
			Name:             "web",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
			OwningRepoID:     "repo-p1a-order",
		}},
	}

	statements := writer.buildTerraformStateStatements(mat)
	retractIdx, mergeIdx := -1, -1
	for i, stmt := range statements {
		if strings.Contains(stmt.Cypher, "MATCHES_STATE") && strings.Contains(stmt.Cypher, "DELETE e") {
			retractIdx = i
		}
		if strings.Contains(stmt.Cypher, "MERGE (c)-[e:MATCHES_STATE]->(s)") {
			mergeIdx = i
		}
	}
	if retractIdx == -1 {
		t.Fatalf("no MATCHES_STATE retract statement found: %#v", statements)
	}
	if mergeIdx == -1 {
		t.Fatalf("no MATCHES_STATE MERGE statement found: %#v", statements)
	}
	if retractIdx > mergeIdx {
		t.Fatalf("MATCHES_STATE retract statement (index %d) ran AFTER the MERGE (index %d), want before", retractIdx, mergeIdx)
	}
}

// fakeConfigMatchResolver reports zero ambiguous candidates for every query,
// the minimal wiring needed so terraformStateMatchesConfigEdgeStatements
// actually emits its MERGE statement in
// TestBuildTerraformStateStatementsRetractsEdgeBeforeMerge (a nil resolver
// leaves ConfigMatchAmbiguous false too, but exercising the resolver path
// keeps this test honest about the wiring a real deployment uses).
type fakeConfigMatchResolver struct{}

func (fakeConfigMatchResolver) CountConfigMatchCandidates(
	_ context.Context,
	queries []TerraformStateConfigMatchQuery,
) (map[string]int, error) {
	counts := make(map[string]int, len(queries))
	for _, q := range queries {
		counts[q.UID] = 1
	}
	return counts, nil
}
