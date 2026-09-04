// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestCollectDeltaAndWholeScopeRefreshRepoIDsStayDisjointAfterDedup guards the
// #6165 review finding on collectDeltaProjectionRepoIDs and
// collectWholeScopeRefreshRepoIDs: nothing IN THIS FILE proves the two
// collectors return disjoint repository sets. Today they are disjoint only
// because of an upstream property this test proves directly: a repository's
// delta-flagged and non-delta whole-scope refresh rows are both emitted under
// the SAME whole-scope partition key
// ("rationale_edges:refresh:v1:whole:"+repoID -- mirrors the unexported
// reducer.repoWideRetractRefreshPartitionKey(reducer.DomainRationaleEdges,
// repoID), see rationale/intents.go), which carries no delta/generation
// component. reducer.LatestIntentsByRepoAndPartition therefore collapses the
// two candidate rows to exactly one survivor per repository per batch before
// RetractEdges ever sees them, so only one of the two rows -- never both --
// can reach collectDeltaProjectionRepoIDs/collectWholeScopeRefreshRepoIDs.
//
// TWO upstream mechanisms are load-bearing here, not one, and the rows below
// are shaped so both are in play. reducer.FilterAuthoritativeIntents runs FIRST
// (shared_projection_worker.go) and keeps only rows matching the accepted
// generation for their acceptance key, so every row surviving to dedup for one
// repository already shares one (scope, unit, run) tuple; the shared partition
// key then collapses that set to one. Both rows below therefore carry the SAME
// non-empty ScopeID/AcceptanceUnitID/SourceRunID -- a production-shaped
// acceptance key. Leaving those blank would make AcceptanceKey() return
// ok=false (SourceRunID has no payload fallback), which both degenerates the
// dedup tuple to all-empty strings AND is a shape FilterAuthoritativeIntents
// drops outright -- collapsing the rows for a reason production never reaches.
//
// This test constructs the dangerous pre-dedup pair (one delta-flagged
// refresh row, one plain whole-scope refresh row, same repository, same
// mirrored partition key), runs it through the REAL dedup function, and then
// runs the REAL collectors against the single survivor. It asserts the
// survivor lands in exactly one collector's output. If a future edit ever
// drops the `delta_projection` exclusion guard inside
// collectWholeScopeRefreshRepoIDs -- the local condition that currently keeps
// it from also claiming a delta-flagged refresh row -- this test goes RED,
// because the delta-flagged row is the one that wins the dedup here.
func TestCollectDeltaAndWholeScopeRefreshRepoIDsStayDisjointAfterDedup(t *testing.T) {
	t.Parallel()

	const repoID = "repo-disjoint-6165"
	// Mirrors reducer.repoWideRetractRefreshPartitionKey(reducer.DomainRationaleEdges, repoID).
	// The helper is unexported, so this cross-package mirror is unavoidable, and
	// a mirror cannot police itself: if the production key scheme ever started
	// varying by delta or generation, both rows below would still share this
	// stale literal, still collapse, and this test would stay green while
	// proving nothing. reducer's TestRepoWideRetractRefreshPartitionKeyShapeIsPinned
	// is the guard for that -- it compares the generator against the same
	// literal in the package that owns it, so a scheme change fails there and
	// names this mirror as needing the lockstep update (#6171 review).
	const wholeScopePartitionKey = "rationale_edges:refresh:v1:whole:" + repoID
	// One accepted generation's acceptance key, shared by both rows: this is
	// what FilterAuthoritativeIntents guarantees about every row that reaches
	// dedup for a repository. SourceRunID in particular has no payload
	// fallback in AcceptanceKey(), so a blank one would degenerate the tuple.
	const (
		scopeID          = "scope-disjoint"
		acceptanceUnitID = "unit-" + repoID
		sourceRunID      = "run-disjoint"
	)

	t0 := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	candidates := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "whole-refresh",
			RepositoryID:     repoID,
			ScopeID:          scopeID,
			AcceptanceUnitID: acceptanceUnitID,
			SourceRunID:      sourceRunID,
			PartitionKey:     wholeScopePartitionKey,
			CreatedAt:        t0,
			Payload: map[string]any{
				"repo_id":     repoID,
				"intent_type": reducer.RepoRefreshIntentType,
			},
		},
		{
			// Later CreatedAt: this row wins LatestIntentsByRepoAndPartition's
			// dedup, so the single survivor fed to the collectors below is the
			// delta-flagged one -- the scenario where a missing guard would let
			// it leak into collectWholeScopeRefreshRepoIDs too.
			IntentID:         "delta-refresh",
			RepositoryID:     repoID,
			ScopeID:          scopeID,
			AcceptanceUnitID: acceptanceUnitID,
			SourceRunID:      sourceRunID,
			PartitionKey:     wholeScopePartitionKey,
			CreatedAt:        t0.Add(time.Second),
			Payload: map[string]any{
				"repo_id":          repoID,
				"intent_type":      reducer.RepoRefreshIntentType,
				"delta_projection": true,
				"delta_file_paths": []string{"a.go"},
			},
		},
	}

	// Guard the shape itself, not just the outcome. If these rows ever lose a
	// real acceptance key, AcceptanceKey() returns ok=false, the dedup tuple
	// degenerates to all-empty strings, and the rows below would still collapse
	// -- but for a reason production never reaches, because
	// FilterAuthoritativeIntents drops key-less rows before dedup sees them.
	// Without this assertion that regression is invisible: the test stays green
	// while proving nothing about the real path.
	for _, candidate := range candidates {
		if _, ok := candidate.AcceptanceKey(); !ok {
			t.Fatalf("candidate %q has no acceptance key, so it is a shape FilterAuthoritativeIntents drops before dedup; the dedup collapse below would not reflect production", candidate.IntentID)
		}
	}

	deduped, superseded := reducer.LatestIntentsByRepoAndPartition(candidates)
	if len(deduped) != 1 {
		t.Fatalf("deduped len = %d, want 1 -- the whole-scope partition key must collapse a repo's delta and non-delta refresh rows to one survivor; got %#v", len(deduped), deduped)
	}
	if len(superseded) != 1 || superseded[0] != "whole-refresh" {
		t.Fatalf("superseded = %v, want [whole-refresh]", superseded)
	}
	if deduped[0].IntentID != "delta-refresh" {
		t.Fatalf("dedup survivor = %q, want %q (test setup assumption broken)", deduped[0].IntentID, "delta-refresh")
	}

	deltaRepos := collectDeltaProjectionRepoIDs(deduped)
	wholeScopeRepos := collectWholeScopeRefreshRepoIDs(deduped)

	inDelta := false
	for _, id := range deltaRepos {
		if id == repoID {
			inDelta = true
		}
	}
	inWholeScope := false
	for _, id := range wholeScopeRepos {
		if id == repoID {
			inWholeScope = true
		}
	}

	if !inDelta {
		t.Fatalf("collectDeltaProjectionRepoIDs(deduped) = %v, want it to contain %q", deltaRepos, repoID)
	}
	if inWholeScope {
		t.Fatalf("collectWholeScopeRefreshRepoIDs(deduped) = %v, want it to NOT contain %q -- repo landed in BOTH collectors, which means both a delta-scoped rewrite AND a whole-repository DELETE would run for it in the same batch", wholeScopeRepos, repoID)
	}
}
