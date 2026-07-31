// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestCanonicalNodeWriterRetractsMatchesStateEdgeOnAuthoritativeNonOwnerDeltaCycleLive
// is the #5623 P1 review FOLLOW-UP real-backend regression: it proves that an
// AUTHORITATIVE non-owner outcome (TerraformStateOwnershipNoOwner or
// TerraformStateOwnershipAmbiguousOwner) on a delta cycle correctly retracts
// a stale MATCHES_STATE edge, unlike TerraformStateOwnershipTransientFailure
// (TestCanonicalNodeWriterPreservesMatchesStateEdgeOnResolverHiccupDeltaCycleLive,
// this file's sibling), which must preserve it.
//
// The review's own framing: a prior fix for the resolver-hiccup accuracy
// regression (filtering the retract's uid set to `row.OwningRepoID != ""`)
// accidentally excluded NoOwner and AmbiguousOwner too, since both ALSO leave
// OwningRepoID empty -- reintroducing the #5623 P0 tenant-visibility leak
// through a narrower door: a backend that previously resolved to a repo and
// later became unowned or ambiguous kept that repo's MATCHES_STATE edge
// indefinitely, and the scoped-token infra predicate
// (infra_scope_grant.go, package query) kept authorizing that repo even
// though it was no longer the unique owner.
//
// Sequence per subtest (mirrors the sibling resolver-hiccup test's structure,
// no read between writes -- the pinned local NornicDB image can silently drop
// a write that follows an interleaved read on the same node within one test
// process, per this file's other tests' own documented reason):
//
//  1. Generation 1 (full): the state resource resolves to a repo that
//     declares a matching TerraformResource. The writer creates the
//     MATCHES_STATE edge.
//  2. Generation 2 (delta): the SAME state resource uid is re-included, but
//     ownership resolution now returns NoOwner (subtest "no owner") or
//     AmbiguousOwner (subtest "ambiguous owner") instead of a resolved repo.
//
// Without the #5623 P1 follow-up fix, both subtests fail: the generation-1
// edge survives because the retract's uid filter (row.OwningRepoID != "")
// excludes NoOwner/AmbiguousOwner rows the same way it correctly excludes
// TransientFailure rows -- collapsing two authoritative answers into the same
// bucket as "we don't know."
func TestCanonicalNodeWriterRetractsMatchesStateEdgeOnAuthoritativeNonOwnerDeltaCycleLive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome projector.TerraformStateOwnershipOutcome
	}{
		{name: "no_owner", outcome: projector.TerraformStateOwnershipNoOwner},
		{name: "ambiguous_owner", outcome: projector.TerraformStateOwnershipAmbiguousOwner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := openBoltTestRunner(t)
			t.Cleanup(func() { runner.close(context.Background()) })

			ownerRepo := "repo-5623-p1-followup-owner-" + tc.name
			const (
				address = "aws_instance.web"
				scopeID = "tf-scope-5623-p1-followup"
			)
			stateUID := "tf-5623-p1-followup-state-" + tc.name
			scope := scopeID + "-" + tc.name

			cleanup := func() {
				if err := boltWriteStatement(
					context.Background(), runner,
					`MATCH (n) WHERE (n:TerraformResource AND n.repo_id = $repo_id) OR (n:TerraformStateResource AND n.uid = $uid) DETACH DELETE n`,
					map[string]any{"repo_id": ownerRepo, "uid": stateUID},
				); err != nil {
					t.Errorf("cleanup: %v", err)
				}
			}
			t.Cleanup(cleanup)
			cleanup()

			ctx := context.Background()
			if err := boltWriteStatement(
				ctx, runner,
				`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
				map[string]any{"repo_id": ownerRepo, "name": address, "path": "envs/" + tc.name + "/main.tf"},
			); err != nil {
				t.Fatalf("seed %s TerraformResource: %v", ownerRepo, err)
			}

			writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 500, nil)

			genOneRow := projector.TerraformStateResourceRow{
				UID: stateUID, Address: address, Mode: "managed", ResourceType: "aws_instance",
				Name: "web", SourceConfidence: facts.SourceConfidenceObserved, CollectorKind: "terraform_state",
				OwningRepoID:     ownerRepo,
				OwnershipOutcome: projector.TerraformStateOwnershipResolved,
			}
			genOne := projector.CanonicalMaterialization{
				ScopeID:                 scope,
				GenerationID:            "tf-generation-5623-p1-followup-1-" + tc.name,
				FirstGeneration:         true,
				TerraformStateResources: []projector.TerraformStateResourceRow{genOneRow},
			}
			if err := writer.Write(ctx, genOne); err != nil {
				t.Fatalf("Write (generation 1, full) error: %v", err)
			}

			// No read between writes -- deliberately, matching this file's
			// other tests' own documented write-loss avoidance.
			//
			// Generation 2: same uid re-included on a DELTA cycle, but
			// ownership resolution now returns an AUTHORITATIVE non-owner
			// outcome (not a transient failure). The row's OwningRepoID stays
			// empty either way -- ONLY the outcome distinguishes "we don't
			// know" from "we know there is no unique owner."
			genTwoRow := genOneRow
			genTwoRow.OwningRepoID = ""
			genTwoRow.OwnershipOutcome = tc.outcome
			genTwo := projector.CanonicalMaterialization{
				ScopeID:                 scope,
				GenerationID:            "tf-generation-5623-p1-followup-2-" + tc.name,
				FirstGeneration:         false,
				DeltaProjection:         true,
				TerraformStateResources: []projector.TerraformStateResourceRow{genTwoRow},
			}
			if err := writer.Write(ctx, genTwo); err != nil {
				t.Fatalf("Write (generation 2, delta, %s) error: %v", tc.name, err)
			}

			edgeCount, err := boltCount(
				ctx, runner,
				`MATCH (c:TerraformResource {repo_id: $repo_id, name: $name})-[e:MATCHES_STATE]->(s:TerraformStateResource {uid: $uid}) RETURN count(e) AS count`,
				map[string]any{"repo_id": ownerRepo, "name": address, "uid": stateUID},
			)
			if err != nil {
				t.Fatalf("count MATCHES_STATE edges: %v", err)
			}
			if edgeCount != 0 {
				t.Fatalf(
					"P1 FOLLOW-UP LEAK REPRODUCED (%s): after a delta cycle where ownership resolution authoritatively returned %s, "+
						"the stale MATCHES_STATE edge to %s survived count=%d, want 0. An authoritative non-owner answer must retract "+
						"the edge, not preserve it -- the retract's uid filter must not collapse this outcome into TransientFailure "+
						"(#5623 P1 review follow-up finding)", tc.name, tc.name, ownerRepo, edgeCount,
				)
			}

			stateNodes, err := runner.runCypher(
				ctx,
				`MATCH (s:TerraformStateResource {uid: $uid}) RETURN s.uid AS uid`,
				map[string]any{"uid": stateUID},
			)
			if err != nil {
				t.Fatalf("read TerraformStateResource node after generation 2: %v", err)
			}
			if len(stateNodes) != 1 {
				t.Fatalf("TerraformStateResource nodes after generation 2 = %d, want 1 (the node must survive; only the edge should be retracted)", len(stateNodes))
			}
		})
	}
}
