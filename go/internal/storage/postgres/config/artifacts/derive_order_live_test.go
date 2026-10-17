// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 (#7126 PR3b-2b) derive ordering and failure semantics: a content write injected between the version read and the list leaves state.content_version = v0 below the version (Classify stale_version); pg_locks and pg_stat_activity show no advisory lock and no open transaction during extraction and one lock inside step 3; an older extractor never replaces a newer extractor's rows; a superseded derive and a failed insert (SQLSTATE 22021) leave the rows and trust columns unchanged; a timeout and a panic write a failure-only state row; about 19 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// advisoryLocks counts the advisory locks granted in this database.
func advisoryLocks(ctx context.Context, t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks
WHERE locktype = 'advisory' AND granted
  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&count); err != nil {
		t.Fatalf("read advisory locks: %v", err)
	}
	return count
}

// openTransactions counts the sessions of this database inside a transaction
// other than the counting one.
func openTransactions(ctx context.Context, t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND pid <> pg_backend_pid() AND xact_start IS NOT NULL`).Scan(&count); err != nil {
		t.Fatalf("read open transactions: %v", err)
	}
	return count
}

// TestDeriveReadsVersionBeforeListLive: a content write that commits between
// step 1 and step 2 is in the rows the derive lists and reads, but the state
// keeps v0, so state.content_version < version and the reader serves the
// repository live (stale_version) until a later derive catches up.
func TestDeriveReadsVersionBeforeListLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(1))
	v0 := versionOf(ctx, t, db, "repo-a")

	deriver := newLiveDeriver(t, db, testExtractorVersion)
	artifacts.SetDeriveHooks(deriver, artifacts.DeriveHooks{AfterVersionRead: func(context.Context) {
		putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(2))
	}})
	result := deriver.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerWrite})
	if result.Outcome != artifacts.OutcomeOK || result.ContentVersion != v0 {
		t.Fatalf("derive = %+v, want ok at v0 = %d", result, v0)
	}
	current := versionOf(ctx, t, db, "repo-a")
	state := stateOf(ctx, t, db, "repo-a")
	if state == nil || state.ContentVersion == nil || *state.ContentVersion != v0 || current != v0+1 {
		t.Fatalf("state = %+v, version = %d; want state.content_version = v0 = %d < version", state, current, v0)
	}
	if got := rowGeneration(t, storedRows(ctx, t, db, "repo-a")); got != 2 {
		t.Fatalf("stored generation = %d, want 2 (the list ran after the injected write)", got)
	}
	reason := artifacts.Classify(&artifacts.State{ContentVersion: state.ContentVersion, ExtractorVersion: state.ExtractorVersion, RowCount: state.RowCount}, &current, testExtractorVersion)
	if reason != artifacts.ReasonStaleVersion {
		t.Fatalf("Classify() = %q, want stale_version: readers must fall back live", reason)
	}
	if result := newLiveDeriver(t, db, testExtractorVersion).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair}); result.Outcome != artifacts.OutcomeOK {
		t.Fatalf("catch-up derive = %+v, want ok", result)
	}
	if state := stateOf(ctx, t, db, "repo-a"); *state.ContentVersion != current {
		t.Fatalf("catch-up state = %+v, want content version %d", state, current)
	}
}

// TestDeriveHoldsTheLockOnlyInStepThreeLive: while step 2 runs (a slow
// extractor), the derive holds no advisory lock and no open transaction; the
// lock is granted inside step 3 and released at commit.
func TestDeriveHoldsTheLockOnlyInStepThreeLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(1))

	var duringLoad, duringLoadTx, duringWrite int
	deriver := newLiveDeriver(t, db, testExtractorVersion)
	artifacts.SetDeriveHooks(deriver, artifacts.DeriveHooks{
		Load: func(ctx context.Context, reader contentartifacts.Reader, repo contentartifacts.Repository, files []contentartifacts.File, hasFileList bool, allowance int) ([]map[string]any, contentartifacts.ReadStats, error) {
			rows, stats, err := contentartifacts.LoadConfigPaths(ctx, reader, repo, files, hasFileList, allowance)
			time.Sleep(200 * time.Millisecond)
			duringLoad = advisoryLocks(ctx, t, db)
			duringLoadTx = openTransactions(ctx, t, db)
			return rows, stats, err
		},
		AfterLock: func(context.Context) { duringWrite = advisoryLocks(ctx, t, db) },
	})
	result := deriver.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerWrite})
	if result.Outcome != artifacts.OutcomeOK {
		t.Fatalf("derive = %+v, want ok", result)
	}
	if duringLoad != 0 || duringLoadTx != 0 {
		t.Fatalf("during step 2: %d advisory locks, %d open transactions; want 0, 0", duringLoad, duringLoadTx)
	}
	if duringWrite != 1 {
		t.Fatalf("during step 3: %d advisory locks, want 1", duringWrite)
	}
	if after := advisoryLocks(ctx, t, db); after != 0 {
		t.Fatalf("after commit: %d advisory locks, want 0", after)
	}
}

// TestDeriveOlderExtractorNeverReplacesNewerRowsLive: rows a newer extractor
// wrote survive an older binary's derive, at the same and at a newer content
// version; the newer binary then advances.
func TestDeriveOlderExtractorNeverReplacesNewerRowsLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(1))
	newer, older := newLiveDeriver(t, db, testExtractorVersion+1), newLiveDeriver(t, db, testExtractorVersion)
	request := artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair}

	if got := newer.DeriveRepository(ctx, request).Outcome; got != artifacts.OutcomeOK {
		t.Fatalf("newer derive = %q, want ok", got)
	}
	before, beforeState := storedRows(ctx, t, db, "repo-a"), stateOf(ctx, t, db, "repo-a")
	if got := older.DeriveRepository(ctx, request).Outcome; got != artifacts.OutcomeSuperseded {
		t.Fatalf("older derive at the same version = %q, want superseded", got)
	}
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(2))
	if got := older.DeriveRepository(ctx, request).Outcome; got != artifacts.OutcomeSuperseded {
		t.Fatalf("older derive at a newer version = %q, want superseded", got)
	}
	if after := storedRows(ctx, t, db, "repo-a"); !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeState, stateOf(ctx, t, db, "repo-a")) {
		t.Fatal("an older extractor's derive changed the rows or state of a newer extractor")
	}
	if got := newer.DeriveRepository(ctx, request).Outcome; got != artifacts.OutcomeOK {
		t.Fatalf("newer derive at the newer version = %q, want ok", got)
	}
	if state := stateOf(ctx, t, db, "repo-a"); state.ExtractorVersion != testExtractorVersion+1 || rowGeneration(t, storedRows(ctx, t, db, "repo-a")) != 2 {
		t.Fatalf("state = %+v, want the newer extractor's rows of generation 2", state)
	}
}

// TestDeriveRollbackLeavesRowsAndStateLive: a superseded derive and a write
// that fails inside step 3 both leave the rows and the trust columns of the
// state row as they were; the failure only counts itself.
func TestDeriveRollbackLeavesRowsAndStateLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(1))
	deriver := newLiveDeriver(t, db, testExtractorVersion)
	request := artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair}
	if got := deriver.DeriveRepository(ctx, request).Outcome; got != artifacts.OutcomeOK {
		t.Fatalf("first derive = %q, want ok", got)
	}
	rows, state := storedRows(ctx, t, db, "repo-a"), stateOf(ctx, t, db, "repo-a")

	if got := deriver.DeriveRepository(ctx, request).Outcome; got != artifacts.OutcomeSuperseded {
		t.Fatalf("repeat derive = %q, want superseded", got)
	}
	if !reflect.DeepEqual(rows, storedRows(ctx, t, db, "repo-a")) || !reflect.DeepEqual(state, stateOf(ctx, t, db, "repo-a")) {
		t.Fatal("a superseded derive changed the rows or the state")
	}

	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(2))
	failing := newLiveDeriver(t, db, testExtractorVersion)
	artifacts.SetDeriveHooks(failing, artifacts.DeriveHooks{Load: func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		// A NUL cannot be stored in a text column (SQLSTATE 22021), so
		// the insert fails after the delete ran inside the transaction.
		return []map[string]any{{"path": "bad\x00path", "relative_path": "docker-compose.yml", "evidence_kind": "docker_compose_env_file"}}, contentartifacts.ReadStats{}, nil
	}})
	result := failing.DeriveRepository(ctx, request)
	if result.Outcome != artifacts.OutcomeError || result.ErrorClass != "sqlstate_22021" || !result.FailureRecorded {
		t.Fatalf("failing derive = %+v, want error class sqlstate_22021 with the failure recorded", result)
	}
	after := stateOf(ctx, t, db, "repo-a")
	if !reflect.DeepEqual(rows, storedRows(ctx, t, db, "repo-a")) {
		t.Fatal("a failed write changed the stored rows")
	}
	if *after.ContentVersion != *state.ContentVersion || *after.RowCount != *state.RowCount || after.ExtractorVersion != state.ExtractorVersion {
		t.Fatalf("state after failure = %+v, want the trust columns of %+v", after, state)
	}
	if after.FailureCount != 1 || after.LastErrorClass == nil || *after.LastErrorClass != "sqlstate_22021" {
		t.Fatalf("state after failure = %+v, want failure_count 1 and class sqlstate_22021", after)
	}
	if got := deriver.DeriveRepository(ctx, request); got.Outcome != artifacts.OutcomeOK || stateOf(ctx, t, db, "repo-a").FailureCount != 0 {
		t.Fatalf("recovery derive = %+v, want ok and failure_count reset", got)
	}
}

// TestDeriveTimeoutAndPanicRecordFailureOnlyLive: a derive that runs past its
// deadline, and one that panics, write a failure-only state row (row_count
// NULL, content_version NULL) on a repository with no state, and no rows.
func TestDeriveTimeoutAndPanicRecordFailureOnlyLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(1))
	slow, err := artifacts.NewDeriver(newLiveDeriverDatabase(db), artifacts.DeriverConfig{
		ExtractorVersion: testExtractorVersion, Timeout: 50 * time.Millisecond,
		Encoding: artifacts.Encoding{Supported: true}, Fence: artifacts.Fence{Supported: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts.SetDeriveHooks(slow, artifacts.DeriveHooks{Load: func(ctx context.Context, _ contentartifacts.Reader, _ contentartifacts.Repository, _ []contentartifacts.File, _ bool, _ int) ([]map[string]any, contentartifacts.ReadStats, error) {
		<-ctx.Done()
		return nil, contentartifacts.ReadStats{}, ctx.Err()
	}})
	if result := slow.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerWrite}); result.Outcome != artifacts.OutcomeTimeout || !result.FailureRecorded {
		t.Fatalf("slow derive = %+v, want timeout with the failure recorded", result)
	}
	panicking := newLiveDeriver(t, db, testExtractorVersion)
	artifacts.SetDeriveHooks(panicking, artifacts.DeriveHooks{AfterLock: func(context.Context) { panic("write bug") }})
	if result := panicking.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerWrite}); result.Outcome != artifacts.OutcomePanic || !result.FailureRecorded {
		t.Fatalf("panicking derive = %+v, want panic with the failure recorded", result)
	}
	state := stateOf(ctx, t, db, "repo-a")
	if state == nil || state.ContentVersion != nil || state.RowCount != nil || state.FailureCount != 2 || *state.LastErrorClass != "panic" {
		t.Fatalf("state = %+v, want a failure-only row with failure_count 2 and class panic", state)
	}
	if rows := storedRows(ctx, t, db, "repo-a"); len(rows) != 0 || advisoryLocks(ctx, t, db) != 0 {
		t.Fatalf("rows = %v after failures; want none and no lock left", rows)
	}
	reason := artifacts.Classify(&artifacts.State{ExtractorVersion: state.ExtractorVersion}, new(int64), testExtractorVersion)
	if reason != artifacts.ReasonFailed {
		t.Fatalf("Classify() = %q, want failed", reason)
	}
	if result := newLiveDeriver(t, db, testExtractorVersion).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair}); result.Outcome != artifacts.OutcomeOK {
		t.Fatalf("derive over a failure-only row = %+v, want ok (Proposed clause)", result)
	}
}
