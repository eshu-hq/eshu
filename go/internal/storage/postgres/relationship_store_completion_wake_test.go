// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Seeded RED test for #8294: rows appended under an already-active generation
// must reopen the succeeded deployable_unit_correlation items whose stored
// repo_ids overlap the appended rows' source/target repos, in the same pass
// (no maintenance listing). The completion wake is one UPDATE keyed off the
// inserted set UpsertResolved reports.
func TestUpsertResolvedReportsInsertedCountAndRepoIDs(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{
		execResults: []sql.Result{
			fakeResultWithRowsAffected{rowsAffected: 1},
			fakeResultWithRowsAffected{rowsAffected: 0},
			fakeResultWithRowsAffected{rowsAffected: 1},
		},
	}
	store := NewRelationshipStore(database)
	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-a",
			TargetRepoID:     "repo-b",
			RelationshipType: relationships.RelDeploysFrom,
			ResolutionSource: relationships.ResolutionSourceInferred,
		},
		{
			SourceRepoID:     "repo-c",
			TargetRepoID:     "repo-d",
			RelationshipType: relationships.RelDeploysFrom,
			ResolutionSource: relationships.ResolutionSourceInferred,
		},
		{
			SourceRepoID:     "repo-a",
			TargetRepoID:     "repo-e",
			RelationshipType: relationships.RelDeploysFrom,
			ResolutionSource: relationships.ResolutionSourceInferred,
		},
	}

	result, err := store.UpsertResolved(context.Background(), "gen-1", resolved)
	if err != nil {
		t.Fatalf("UpsertResolved() error = %v", err)
	}
	if result.Inserted != 2 {
		t.Fatalf("UpsertResolved() inserted = %d, want 2 (one row conflicted away)", result.Inserted)
	}
	for _, want := range []string{"repo-a", "repo-b", "repo-e"} {
		found := false
		for _, got := range result.RepoIDs {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("UpsertResolved() repo IDs = %v, want %q (inserted rows only)", result.RepoIDs, want)
		}
	}
	for _, got := range result.RepoIDs {
		if got == "repo-c" || got == "repo-d" {
			t.Fatalf("UpsertResolved() repo IDs = %v, must not contain repos of the conflicted row", result.RepoIDs)
		}
	}
}

func TestUpsertResolvedEmptyReportsZeroResult(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{}
	store := NewRelationshipStore(database)

	result, err := store.UpsertResolved(context.Background(), "gen-1", nil)
	if err != nil {
		t.Fatalf("UpsertResolved(nil) error = %v", err)
	}
	if result.Inserted != 0 || len(result.RepoIDs) != 0 {
		t.Fatalf("UpsertResolved(nil) = %+v, want zero result", result)
	}
	if len(database.execs) != 0 {
		t.Fatalf("UpsertResolved(nil) issued %d statements, want none", len(database.execs))
	}
}

// The completion wake reopens every succeeded deployable_unit_correlation item
// whose recorded fingerprint repo_ids overlap the inserted repos, in one
// UPDATE with no listing. Empty input is a no-op without touching the database.
func TestReopenDeployableUnitCorrelationsForReposEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{}
	store := NewRelationshipStore(database)

	reopened, err := store.ReopenDeployableUnitCorrelationsForRepos(context.Background(), nil)
	if err != nil {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos(nil) error = %v", err)
	}
	if reopened != 0 {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos(nil) = %d, want 0", reopened)
	}
	if len(database.execs) != 0 {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos(nil) issued %d statements, want none", len(database.execs))
	}
}

func TestReopenDeployableUnitCorrelationsForReposReopensOverlappingSucceeded(t *testing.T) {
	t.Parallel()

	database := &fakeExecQueryer{
		execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 2}},
	}
	store := NewRelationshipStore(database)

	reopened, err := store.ReopenDeployableUnitCorrelationsForRepos(
		context.Background(), []string{"repo-a", "repo-b"},
	)
	if err != nil {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos() error = %v", err)
	}
	if reopened != 2 {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos() = %d, want 2", reopened)
	}
	if len(database.execs) != 1 {
		t.Fatalf("wake issued %d statements, want exactly 1 (no listing)", len(database.execs))
	}
	query := database.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"domain = 'deployable_unit_correlation'",
		"status = 'succeeded'",
		"deployable_unit_reopen_fingerprints",
		"repo_ids &&",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("wake query missing %q:\n%s", want, query)
		}
	}
}

// The wake reuses the shared succeeded-to-pending SET clause (#7731) so it
// resets every state column the reducer claim path reads. A drift between the
// wake and the shared clause fails here instead of silently narrowing a reopen.
func TestCompletionWakeQueryComposesSharedReopenClause(t *testing.T) {
	t.Parallel()

	if !strings.Contains(reopenDeployableUnitCorrelationsForReposQuery, ReopenSucceededReducerSetClause) {
		t.Fatal("completion wake query does not compose ReopenSucceededReducerSetClause")
	}
}
