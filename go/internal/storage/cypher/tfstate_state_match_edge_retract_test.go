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

// TestTerraformStateMatchesConfigEdgeRetractStatementsRunsUnderDeltaProjection
// proves the #5623 fix: UNLIKE terraformStateResourceRetractStatements' own
// DeltaProjection skip (which exists because that whole-population DETACH
// DELETE would mass-delete every state resource a delta cycle did not touch),
// this edge-level retract still emits its statement on a delta cycle. Its
// `s.generation_id = $generation_id` anchor already restricts it to state
// resources upserted THIS exact generation (buildTerraformStateStatements
// always runs the resource upsert first, delta or full), so it never touches
// a resource this delta cycle did not process -- there is no mass-deletion
// risk to guard against. Skipping it on delta cycles instead left a real
// window: a resource whose OwningRepoID changes on a delta cycle gets its NEW
// MATCHES_STATE edge written immediately (the MERGE has no DeltaProjection
// guard) but kept its OLD edge to the now-orphaned repo until the next full
// reconciliation, so the state resource carried two edges to two different
// repos simultaneously and infra_scope_grant.go's scope predicate -- which
// assumes at most one edge -- admitted it for either repo's grant (a
// tenant-visibility leak; see TestLiveInfraScopeShapeMatchesStateStaleEdgeExcluded
// in internal/query for the live proof of the leak this closes). The row
// carries a resolved OwningRepoID so it is included in the retract's uid set
// (see TestTerraformStateMatchesConfigEdgeRetractStatementsExcludesUnresolvedOwnershipRows
// for the #5623 P1 review fix that excludes unresolved rows).
func TestTerraformStateMatchesConfigEdgeRetractStatementsRunsUnderDeltaProjection(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:         "tf-scope-p1a",
		GenerationID:    "tf-generation-p1a-2",
		FirstGeneration: false,
		DeltaProjection: true,
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:              "tf-resource-p1a-2",
			Address:          "aws_instance.web",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
			OwningRepoID:     "repo-p1a-2",
			OwnershipOutcome: projector.TerraformStateOwnershipResolved,
		}},
	}

	statements := writer.terraformStateMatchesConfigEdgeRetractStatements(mat)
	if got, want := len(statements), 1; got != want {
		t.Fatalf("terraformStateMatchesConfigEdgeRetractStatements() under DeltaProjection = %d statements, want %d (the retract must run on delta cycles too, #5623): %#v", got, want, statements)
	}
	stmt := statements[0]
	if !strings.Contains(stmt.Cypher, "s.generation_id = $generation_id") {
		t.Fatalf("Cypher = %q, want the generation anchor that scopes this retract to resources upserted THIS cycle (the property that makes running on delta cycles safe)", stmt.Cypher)
	}
	if got, want := stmt.Parameters["generation_id"], "tf-generation-p1a-2"; got != want {
		t.Fatalf("generation_id = %#v, want %q", got, want)
	}
	uids, ok := stmt.Parameters["uids"].([]string)
	if !ok || len(uids) != 1 || uids[0] != "tf-resource-p1a-2" {
		t.Fatalf("uids = %#v, want [tf-resource-p1a-2]", stmt.Parameters["uids"])
	}
}

// TestTerraformStateMatchesConfigEdgeRetractStatementsExcludesUnresolvedOwnershipRows
// proves the #5623 P1 review fix: "this generation upserted the node" is not
// the same fact as "we know its correct owner this cycle." A row whose
// OwnershipOutcome is TerraformStateOwnershipTransientFailure this cycle
// (TerraformStateOwnershipResolver.ResolveOwningRepoID fails closed on ANY
// resolver error, including an ordinary transient Postgres timeout, not just
// genuine "no owner") must NOT have its existing MATCHES_STATE edge
// retracted -- doing so would wipe a still-correct edge on an ordinary
// resolver hiccup instead of leaving it alone until a future cycle resolves
// successfully. Proves both the all-unresolved case (0 statements) and the
// mixed case (the resolved row's uid is included, the unresolved row's uid is
// not).
func TestTerraformStateMatchesConfigEdgeRetractStatementsExcludesUnresolvedOwnershipRows(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)

	t.Run("all rows unresolved emits nothing", func(t *testing.T) {
		t.Parallel()
		mat := projector.CanonicalMaterialization{
			ScopeID:         "tf-scope-p1-unresolved",
			GenerationID:    "tf-generation-p1-unresolved",
			FirstGeneration: false,
			DeltaProjection: true,
			TerraformStateResources: []projector.TerraformStateResourceRow{{
				UID:              "tf-resource-p1-unresolved",
				Address:          "aws_instance.web",
				SourceConfidence: facts.SourceConfidenceObserved,
				CollectorKind:    "terraform_state",
				OwningRepoID:     "", // resolver hiccup this cycle
				OwnershipOutcome: projector.TerraformStateOwnershipTransientFailure,
			}},
		}
		statements := writer.terraformStateMatchesConfigEdgeRetractStatements(mat)
		if len(statements) != 0 {
			t.Fatalf("terraformStateMatchesConfigEdgeRetractStatements() with only unresolved rows = %d statements, want 0 (a resolver hiccup must not wipe an existing edge): %#v", len(statements), statements)
		}
	})

	t.Run("mixed resolved and unresolved rows includes only the resolved uid", func(t *testing.T) {
		t.Parallel()
		mat := projector.CanonicalMaterialization{
			ScopeID:         "tf-scope-p1-mixed",
			GenerationID:    "tf-generation-p1-mixed",
			FirstGeneration: false,
			DeltaProjection: true,
			TerraformStateResources: []projector.TerraformStateResourceRow{
				{
					UID:              "tf-resource-p1-mixed-resolved",
					Address:          "aws_instance.web",
					SourceConfidence: facts.SourceConfidenceObserved,
					CollectorKind:    "terraform_state",
					OwningRepoID:     "repo-p1-mixed-resolved",
					OwnershipOutcome: projector.TerraformStateOwnershipResolved,
				},
				{
					UID:              "tf-resource-p1-mixed-unresolved",
					Address:          "aws_instance.db",
					SourceConfidence: facts.SourceConfidenceObserved,
					CollectorKind:    "terraform_state",
					OwningRepoID:     "", // resolver hiccup this cycle
					OwnershipOutcome: projector.TerraformStateOwnershipTransientFailure,
				},
			},
		}
		statements := writer.terraformStateMatchesConfigEdgeRetractStatements(mat)
		if got, want := len(statements), 1; got != want {
			t.Fatalf("statements = %d, want %d: %#v", got, want, statements)
		}
		uids, ok := statements[0].Parameters["uids"].([]string)
		if !ok || len(uids) != 1 || uids[0] != "tf-resource-p1-mixed-resolved" {
			t.Fatalf("uids = %#v, want exactly [tf-resource-p1-mixed-resolved] (the unresolved row's uid must be excluded)", statements[0].Parameters["uids"])
		}
	})
}

// TestTerraformStateMatchesConfigEdgeRetractStatementsIncludesAuthoritativeNonOwnerRows
// is the #5623 P1 review FOLLOW-UP finding: NoOwner and AmbiguousOwner are
// AUTHORITATIVE answers, not failures, and must be INCLUDED in the retract's
// uid set -- the opposite of TransientFailure above. A prior version of this
// fix's own filter (row.OwningRepoID != "") accidentally excluded these two
// outcomes too, since both also leave OwningRepoID empty, reintroducing the
// #5623 P0 tenant-visibility leak through a narrower door: a backend that
// previously resolved to a repo and later became unowned or ambiguous kept
// that repo's MATCHES_STATE edge indefinitely.
func TestTerraformStateMatchesConfigEdgeRetractStatementsIncludesAuthoritativeNonOwnerRows(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)

	for _, tc := range []struct {
		name    string
		outcome projector.TerraformStateOwnershipOutcome
	}{
		{name: "no owner", outcome: projector.TerraformStateOwnershipNoOwner},
		{name: "ambiguous owner", outcome: projector.TerraformStateOwnershipAmbiguousOwner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mat := projector.CanonicalMaterialization{
				ScopeID:         "tf-scope-p1-authoritative-" + tc.name,
				GenerationID:    "tf-generation-p1-authoritative-" + tc.name,
				FirstGeneration: false,
				DeltaProjection: true,
				TerraformStateResources: []projector.TerraformStateResourceRow{{
					UID:              "tf-resource-p1-authoritative",
					Address:          "aws_instance.web",
					SourceConfidence: facts.SourceConfidenceObserved,
					CollectorKind:    "terraform_state",
					OwningRepoID:     "", // no unique owner this cycle
					OwnershipOutcome: tc.outcome,
				}},
			}
			statements := writer.terraformStateMatchesConfigEdgeRetractStatements(mat)
			if got, want := len(statements), 1; got != want {
				t.Fatalf("statements = %d, want %d (an authoritative %s answer must retract a stale edge, not preserve it): %#v", got, want, tc.name, statements)
			}
			uids, ok := statements[0].Parameters["uids"].([]string)
			if !ok || len(uids) != 1 || uids[0] != "tf-resource-p1-authoritative" {
				t.Fatalf("uids = %#v, want [tf-resource-p1-authoritative]", statements[0].Parameters["uids"])
			}
		})
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
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:              "tf-resource-p1a-3",
			Address:          "aws_instance.web",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
			OwningRepoID:     "repo-p1a-3",
			OwnershipOutcome: projector.TerraformStateOwnershipResolved,
		}},
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
	if !strings.Contains(stmt.Cypher, "s.uid IN $uids") {
		t.Fatalf("Cypher = %q, want the uid filter that restricts this retract to rows whose ownership actually resolved this cycle (#5623 P1 review fix)", stmt.Cypher)
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
	if uids, ok := stmt.Parameters["uids"].([]string); !ok || len(uids) != 1 || uids[0] != "tf-resource-p1a-3" {
		t.Fatalf("uids = %#v, want [tf-resource-p1a-3]", stmt.Parameters["uids"])
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
			OwnershipOutcome: projector.TerraformStateOwnershipResolved,
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
