// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestResolvedByReposWithCorpusFenceSQLFusesFenceAndRead pins the #6740
// shape: ONE statement carries the shipped fence predicate in a materialized
// CTE and the shipped by-repos filter, so the verdict and the rows share a
// statement snapshot. Both halves are derived from the shipped constants, not
// copied, so the fence here cannot drift from the boolean and holder queries.
func TestResolvedByReposWithCorpusFenceSQLFusesFenceAndRead(t *testing.T) {
	t.Parallel()

	query := listResolvedByReposWithCorpusFenceSQL
	for _, want := range []string{
		"WITH fence AS MATERIALIZED",
		incompleteScopeRelationshipGenerationsPredicate,
		"WHERE g.status = 'active'",
		"r.source_repo_id IN (%s)",
		"OR r.target_repo_id IN (%s)",
		") AS r ON fence.complete",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("fused query missing %q:\n%s", want, query)
		}
	}
	if got := strings.Count(query, ";"); got != 0 {
		t.Fatalf("fused query has %d statement separators, want one statement", got)
	}
}

func TestRelationshipStoreGetResolvedRelationshipsForReposWithCorpusFence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	seed := func(t *testing.T, currentStatus string) *RelationshipStore {
		t.Helper()
		database := newRelationshipTestDB()
		database.scopes["scope-infra"] = scopeRecord{status: "active", activeGenerationID: "gen-infra"}
		store := NewRelationshipStore(database)
		if err := store.ActivateResolutionGeneration(ctx, "gen-infra", "scope-infra"); err != nil {
			t.Fatalf("ActivateResolutionGeneration: %v", err)
		}
		if err := store.UpsertResolved(ctx, "gen-infra", []relationships.ResolvedRelationship{{
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-api",
			RelationshipType: relationships.RelProvisionsDependencyFor,
			Confidence:       0.94,
			EvidenceCount:    2,
			Rationale:        "infra provisions api",
			ResolutionSource: relationships.ResolutionSourceInferred,
			Details:          map[string]any{"evidence_kinds": []any{"TERRAFORM_ECS_SERVICE"}},
		}}); err != nil {
			t.Fatalf("UpsertResolved: %v", err)
		}
		gen := database.generations["gen-infra"]
		gen.status = currentStatus
		database.generations["gen-infra"] = gen
		return store
	}

	t.Run("complete corpus returns the matching rows", func(t *testing.T) {
		t.Parallel()
		rows, complete, err := seed(t, "active").GetResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-api", " ", "repo-api"})
		if err != nil || !complete {
			t.Fatalf("read = (complete=%v, err=%v), want (true, nil)", complete, err)
		}
		if len(rows) != 1 {
			t.Fatalf("rows = %#v, want one", rows)
		}
		got := rows[0]
		if got.SourceRepoID != "repo-infra" || got.TargetRepoID != "repo-api" ||
			got.RelationshipType != relationships.RelProvisionsDependencyFor ||
			got.Confidence != 0.94 || got.EvidenceCount != 2 || got.Rationale != "infra provisions api" ||
			got.ResolutionSource != relationships.ResolutionSourceInferred || got.Details["evidence_kinds"] == nil {
			t.Fatalf("row = %#v, want every column carried through", got)
		}
	})

	t.Run("complete corpus with no match returns no rows", func(t *testing.T) {
		t.Parallel()
		rows, complete, err := seed(t, "active").GetResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-unrelated"})
		if err != nil || !complete || len(rows) != 0 {
			t.Fatalf("read = (%#v, complete=%v, err=%v), want ([], true, nil)", rows, complete, err)
		}
	})

	t.Run("incomplete corpus returns no rows and false", func(t *testing.T) {
		t.Parallel()
		rows, complete, err := seed(t, "pending").GetResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-api"})
		if err != nil || complete || rows != nil {
			t.Fatalf("read = (%#v, complete=%v, err=%v), want (nil, false, nil)", rows, complete, err)
		}
	})

	t.Run("empty repository set still evaluates the fence", func(t *testing.T) {
		t.Parallel()
		if _, complete, err := seed(t, "pending").GetResolvedRelationshipsForReposWithCorpusFence(ctx, nil); err != nil || complete {
			t.Fatalf("empty read = (complete=%v, err=%v), want (false, nil)", complete, err)
		}
		if _, complete, err := seed(t, "active").GetResolvedRelationshipsForReposWithCorpusFence(ctx, nil); err != nil || !complete {
			t.Fatalf("empty read = (complete=%v, err=%v), want (true, nil)", complete, err)
		}
	})
}

func TestScanCorpusFencedResolvedRowsRejectsEmptyResult(t *testing.T) {
	t.Parallel()

	_, complete, err := scanCorpusFencedResolvedRows(newRelationshipRows(nil))
	if !errors.Is(err, errCorpusFencedResolvedNoRows) || complete {
		t.Fatalf("scan(no rows) = (complete=%v, err=%v), want the no-rows error and no verdict", complete, err)
	}
}

// TestRelationshipStoreSatisfiesCorpusFencedResolvedRelationshipLoader pins the
// production wiring: cmd/reducer hands this store to both the workload
// projection input loader and the deployable-unit correlation handler, and
// they take the fused single-snapshot read only through this optional
// interface. A store that kept the repo-scoped read but lost the fenced one
// would silently fall back to the two-statement fence and its #6740 window.
func TestRelationshipStoreSatisfiesCorpusFencedResolvedRelationshipLoader(t *testing.T) {
	t.Parallel()

	var loader reducer.ResolvedRelationshipLoader = NewRelationshipStore(newRelationshipTestDB())
	if _, ok := loader.(reducer.CorpusFencedResolvedRelationshipLoader); !ok {
		t.Fatal("RelationshipStore does not implement reducer.CorpusFencedResolvedRelationshipLoader")
	}
	if _, ok := loader.(reducer.RepositoryScopedResolvedRelationshipLoader); !ok {
		t.Fatal("RelationshipStore does not implement reducer.RepositoryScopedResolvedRelationshipLoader")
	}
}
