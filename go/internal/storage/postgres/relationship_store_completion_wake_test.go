// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// Regression test for #8294: UpsertResolved reports how many rows it
// inserted plus the full resolved repo scope, so the completion wake can
// run over that scope on every pass — including a retry that inserted
// nothing — while its fingerprint verdict decides what reopens.
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
	// RepoIDs is the wake scope: every row in the upsert set, including the
	// conflicted row's repos, so a retry that inserts nothing still re-runs
	// the wake over the full resolved scope (#8294 review F3).
	for _, want := range []string{"repo-a", "repo-b", "repo-c", "repo-d", "repo-e"} {
		found := false
		for _, got := range result.RepoIDs {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("UpsertResolved() repo IDs = %v, want %q (full upsert scope)", result.RepoIDs, want)
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

	woken, err := store.ReopenDeployableUnitCorrelationsForRepos(context.Background(), nil)
	if err != nil {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos(nil) error = %v", err)
	}
	if woken.Reopened != 0 || woken.ReplayFlagged != 0 {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos(nil) = %+v, want zero", woken)
	}
	if len(database.execs) != 0 {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos(nil) issued %d statements, want none", len(database.execs))
	}
}

func TestReopenDeployableUnitCorrelationsForReposReopensOverlappingSucceeded(t *testing.T) {
	t.Parallel()

	database := &fakeBeginnerExecQueryer{fakeExecQueryer: fakeExecQueryer{
		execResults: []sql.Result{
			fakeResultWithRowsAffected{rowsAffected: 0}, // set lock_timeout
			fakeResultWithRowsAffected{rowsAffected: 1}, // in-flight flag arm
			fakeResultWithRowsAffected{rowsAffected: 2}, // succeeded reopen arm
		},
	}}
	store := NewRelationshipStore(database)

	woken, err := store.ReopenDeployableUnitCorrelationsForRepos(
		context.Background(), []string{"repo-a", "repo-b"},
	)
	if err != nil {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos() error = %v", err)
	}
	if woken.Reopened != 2 || woken.ReplayFlagged != 1 {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos() = %+v, want {Reopened:2 ReplayFlagged:1}", woken)
	}
	if !database.committed || database.rolledBack {
		t.Fatalf("wake transaction committed=%v rolledBack=%v, want committed only", database.committed, database.rolledBack)
	}
	if len(database.execs) != 3 {
		t.Fatalf("wake issued %d statements, want 3 (lock_timeout, flag arm, reopen arm; no listing)", len(database.execs))
	}
	if !strings.Contains(database.execs[0].query, "lock_timeout") {
		t.Fatalf("first wake statement does not bound lock_timeout:\n%s", database.execs[0].query)
	}
	flagQuery := database.execs[1].query
	for _, want := range []string{
		"cross_scope_replay_required = TRUE",
		"status IN ('claimed', 'running')",
	} {
		if !strings.Contains(flagQuery, want) {
			t.Fatalf("flag-arm query missing %q:\n%s", want, flagQuery)
		}
	}
	updateAt := strings.Index(flagQuery, "UPDATE fact_work_items")
	setClause := flagQuery[strings.Index(flagQuery[updateAt:], "SET ")+updateAt : strings.Index(flagQuery[updateAt:], "WHERE ")+updateAt]
	if strings.Contains(setClause, "status") {
		t.Fatalf("flag arm SET must not name status (ACK trigger is UPDATE OF status):\n%s", flagQuery)
	}
	reopenQuery := database.execs[2].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"work.domain = 'deployable_unit_correlation'",
		"work.status = 'succeeded'",
		"deployable_unit_reopen_fingerprints",
		"repo_ids &&",
		"fingerprint.work_item_id IS NULL",
	} {
		if !strings.Contains(reopenQuery, want) {
			t.Fatalf("reopen-arm query missing %q:\n%s", want, reopenQuery)
		}
	}
}

// The in-flight flag arm must require a recorded fingerprint row: a
// claimed/running item with no fingerprint row is mid-first-execution (its
// handler records the fingerprint only after the admission write, so a
// blocked or killed-then-reclaimed claim has none yet). Flagging it forces a
// redundant replay, and the migration 093 ACK trigger's attempt reset then
// erases the replacement's reclaim evidence, failing the kill-worker
// fault cell's killed-claims-reexecuted proof (want attempt_count above the
// kill-time value; got the reset). The succeeded arm still reopens missing
// fingerprints (pinned by the live missing_fingerprint_row_reopens proof);
// only the in-flight arm needs recorded input to prove staleness.
func TestCompletionWakeFlagArmRequiresRecordedFingerprint(t *testing.T) {
	t.Parallel()

	database := &fakeBeginnerExecQueryer{fakeExecQueryer: fakeExecQueryer{
		execResults: []sql.Result{
			fakeResultWithRowsAffected{rowsAffected: 0}, // set lock_timeout
			fakeResultWithRowsAffected{rowsAffected: 0}, // in-flight flag arm
			fakeResultWithRowsAffected{rowsAffected: 0}, // succeeded reopen arm
		},
	}}
	store := NewRelationshipStore(database)

	if _, err := store.ReopenDeployableUnitCorrelationsForRepos(
		context.Background(), []string{"repo-a"},
	); err != nil {
		t.Fatalf("ReopenDeployableUnitCorrelationsForRepos() error = %v", err)
	}
	if len(database.execs) != 3 {
		t.Fatalf("wake issued %d statements, want 3 (lock_timeout, flag arm, reopen arm)", len(database.execs))
	}
	flagQuery := database.execs[1].query
	if !strings.Contains(flagQuery, "fingerprint.work_item_id IS NOT NULL") {
		t.Fatalf("flag arm does not require a recorded fingerprint row; "+
			"a claimed/running item with no fingerprint row would be flagged:\n%s", flagQuery)
	}
	for _, want := range []string{
		"work.status IN ('claimed', 'running')",
		"NOT work.cross_scope_replay_required",
		"repo_ids &&",
	} {
		if !strings.Contains(flagQuery, want) {
			t.Fatalf("flag arm missing %q:\n%s", want, flagQuery)
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

// Both wake arms compose the single scope replay floor text and lock their
// candidates in work_item_id C order (#8294 review F1/F2). A hand copy of
// the floor, or an unordered multi-row lock, fails here; the floor's live
// behavior is proven on Postgres.
func TestCompletionWakeQueriesComposeFloorAndOrderedLock(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"flag":   flagInflightDeployableUnitCorrelationsQuery,
		"reopen": reopenDeployableUnitCorrelationsForReposQuery,
	} {
		if !strings.Contains(query, backfill.ScopeReplayFloorCTE) {
			t.Errorf("%s arm does not compose backfill.ScopeReplayFloorCTE", name)
		}
		if !strings.Contains(query, backfill.ScopeReplayFloorJoins) {
			t.Errorf("%s arm does not compose backfill.ScopeReplayFloorJoins", name)
		}
		for _, want := range []string{
			"work_generation.status <> 'failed'",
			">= (floor.floor_ingested_at, floor.floor_generation_id)",
			`ORDER BY work.work_item_id COLLATE "C"`,
			"FOR UPDATE OF work",
			backfill.ResolvedInputFingerprintSQL("fingerprint.repo_ids"),
		} {
			if !strings.Contains(query, want) {
				t.Errorf("%s arm missing %q", name, want)
			}
		}
	}
}

// Lock contention (55P03/40P01) classifies retryable; every other error
// passes through unchanged (#8294 review F2).
func TestClassifyCompletionWakeError(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"55P03", "40P01"} {
		err := classifyCompletionWakeError(&pgconn.PgError{Code: code})
		var contention CompletionWakeContentionError
		if !errors.As(err, &contention) || !contention.Retryable() {
			t.Fatalf("classify(%s) = %v, want retryable CompletionWakeContentionError", code, err)
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Fatalf("classify(%s) lost the driver error: %v", code, err)
		}
	}
	plain := errors.New("boom")
	if got := classifyCompletionWakeError(plain); got != plain {
		t.Fatalf("classify(plain) = %v, want unchanged", got)
	}
}
