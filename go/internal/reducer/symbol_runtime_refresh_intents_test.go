// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"
)

func perEdgeRowForRepo(domain, repoID, partitionKey string) SharedProjectionIntentRow {
	return SharedProjectionIntentRow{
		ProjectionDomain: domain,
		PartitionKey:     partitionKey,
		ScopeID:          "scope-a",
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload:          map[string]any{"repo_id": repoID, "action": "upsert"},
	}
}

// TestBuildRepoWideRetractRefreshIntentsPairsOnePerRepo proves the paired-emission
// invariant the worker fence relies on: exactly one whole-scope refresh intent is
// emitted per repository that has per-edge rows, carrying the repo_refresh marker,
// the refresh action, and the same acceptance unit/source run as the per-edge
// rows, under the deterministic whole-scope partition key.
func TestBuildRepoWideRetractRefreshIntentsPairsOnePerRepo(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.June, 18, 12, 0, 0, 0, time.UTC)
	perEdge := []SharedProjectionIntentRow{
		perEdgeRowForRepo(DomainHandlesRoute, "repo-a", "fn1->repo-a:/x"),
		perEdgeRowForRepo(DomainHandlesRoute, "repo-a", "fn2->repo-a:/y"),
		perEdgeRowForRepo(DomainHandlesRoute, "repo-b", "fn3->repo-b:/z"),
	}
	contextByRepoID := map[string]ProjectionContext{
		"repo-a": {ScopeID: "scope-a", SourceRunID: "run-1", GenerationID: "gen-1"},
		"repo-b": {ScopeID: "scope-a", SourceRunID: "run-1", GenerationID: "gen-1"},
	}

	refreshes := buildRepoWideRetractRefreshIntents(
		DomainHandlesRoute, perEdge, contextByRepoID, createdAt, handlesRouteEvidenceSource,
	)

	if len(refreshes) != 2 {
		t.Fatalf("refresh intent count = %d, want 2 (one per repo)", len(refreshes))
	}
	byRepo := map[string]SharedProjectionIntentRow{}
	for _, r := range refreshes {
		byRepo[r.RepositoryID] = r
	}
	for _, repoID := range []string{"repo-a", "repo-b"} {
		r, ok := byRepo[repoID]
		if !ok {
			t.Fatalf("missing refresh intent for %q", repoID)
		}
		if !isRepoRefreshRow(r) {
			t.Errorf("%q refresh intent not marked repo_refresh", repoID)
		}
		if got := payloadStr(r.Payload, "action"); got != repoRefreshAction {
			t.Errorf("%q refresh action = %q, want %q", repoID, got, repoRefreshAction)
		}
		if want := repoWideRetractRefreshPartitionKey(DomainHandlesRoute, repoID); r.PartitionKey != want {
			t.Errorf("%q refresh partition key = %q, want %q", repoID, r.PartitionKey, want)
		}
		// The refresh intent must share the acceptance key the per-edge rows carry,
		// or the fence lookup can never match.
		if r.AcceptanceUnitID != repoID || r.SourceRunID != "run-1" || r.ScopeID != "scope-a" {
			t.Errorf("%q refresh acceptance identity = (%q,%q,%q), want (scope-a,%q,run-1)",
				repoID, r.ScopeID, r.AcceptanceUnitID, r.SourceRunID, repoID)
		}
	}
}

// TestBuildRepoWideRetractRefreshIntentsSkipsNonRepoWideDomain confirms refresh
// intents are emitted only for repo-wide-retract domains; a file/repo-keyed
// domain must stay byte-identical (no refresh intents).
func TestBuildRepoWideRetractRefreshIntentsSkipsNonRepoWideDomain(t *testing.T) {
	t.Parallel()

	perEdge := []SharedProjectionIntentRow{perEdgeRowForRepo(DomainCodeCalls, "repo-a", "pk")}
	contextByRepoID := map[string]ProjectionContext{"repo-a": {ScopeID: "scope-a", SourceRunID: "run-1", GenerationID: "gen-1"}}

	if got := buildRepoWideRetractRefreshIntents(DomainCodeCalls, perEdge, contextByRepoID, time.Now(), codeCallEvidenceSource); got != nil {
		t.Fatalf("expected no refresh intents for non-repo-wide domain, got %d", len(got))
	}
}
