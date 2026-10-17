// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"slices"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// duplicateLoad returns a step 2 whose extractor output holds a row twice:
// a duplicate of (raw_path, relative_path, evidence_kind), the triple
// row_digest is built from. The extractors never emit this (each deduplicates
// on the triple); the proof forces it to show that a duplicate is a loud
// failure and not a silent skew between row_count and the stored rows.
func duplicateLoad(rows ...map[string]any) func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
	return func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		return rows, contentartifacts.ReadStats{FilesListed: 1, CandidateCount: 1, CandidatesHydrated: 1}, nil
	}
}

func dupComposeRow(path string) map[string]any {
	return map[string]any{"path": path, "source_repo": "", "relative_path": "docker-compose.yml", "evidence_kind": "docker_compose_env_file"}
}

// TestDeriveDuplicateRowDigestFailsLoudlyLive: extractor output that holds
// one triple twice makes the derive fail with SQLSTATE 23505, never report ok
// with fewer stored rows than row_count. The step 3 transaction rolls back, so
// the previous rows, content_version, extractor_version and row_count stay,
// and only the failure columns change. A first derive of a repository with a
// duplicate leaves a failure-only state row and no rows.
func TestDeriveDuplicateRowDigestFailsLoudlyLive(t *testing.T) {
	ctx, db := openDatabase(t)
	dup := []map[string]any{dupComposeRow("env/a.env"), dupComposeRow("env/b.env"), dupComposeRow("env/a.env")}

	t.Run("replacing_existing_rows", func(t *testing.T) {
		putFile(ctx, t, db, "repo-dup", "docker-compose.yml", composeBody(1))
		if got := newLiveDeriver(t, db, testExtractorVersion).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-dup"}).Outcome; got != artifacts.OutcomeOK {
			t.Fatalf("first derive = %q, want ok", got)
		}
		before := stateOf(ctx, t, db, "repo-dup")
		rowsBefore := storedRows(ctx, t, db, "repo-dup")
		putFile(ctx, t, db, "repo-dup", "docker-compose.yml", composeBody(2))

		deriver := newLiveDeriver(t, db, testExtractorVersion+1)
		artifacts.SetDeriveHooks(deriver, artifacts.DeriveHooks{Load: duplicateLoad(dup...)})
		result := deriver.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-dup", Trigger: artifacts.TriggerWrite})
		if result.Outcome != artifacts.OutcomeError || result.ErrorClass != "sqlstate_23505" || !result.FailureRecorded {
			t.Fatalf("derive of a duplicate triple = %+v, want error class sqlstate_23505 with the failure recorded", result)
		}
		after := stateOf(ctx, t, db, "repo-dup")
		if after.FailureCount != before.FailureCount+1 || after.LastErrorClass == nil || *after.LastErrorClass != "sqlstate_23505" {
			t.Fatalf("state after = %+v, want one more failure of class sqlstate_23505 (before %+v)", after, before)
		}
		if *after.ContentVersion != *before.ContentVersion || after.ExtractorVersion != before.ExtractorVersion || *after.RowCount != *before.RowCount {
			t.Fatalf("state after = %+v, want content_version, extractor_version and row_count of %+v", after, before)
		}
		if got := storedRows(ctx, t, db, "repo-dup"); !slices.Equal(got, rowsBefore) {
			t.Fatalf("stored rows = %v, want the previous rows %v", got, rowsBefore)
		}
	})

	t.Run("first_derive", func(t *testing.T) {
		putFile(ctx, t, db, "repo-dup-first", "docker-compose.yml", composeBody(1))
		deriver := newLiveDeriver(t, db, testExtractorVersion)
		artifacts.SetDeriveHooks(deriver, artifacts.DeriveHooks{Load: duplicateLoad(dup...)})
		result := deriver.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-dup-first"})
		if result.Outcome != artifacts.OutcomeError || result.ErrorClass != "sqlstate_23505" || !result.FailureRecorded {
			t.Fatalf("derive of a duplicate triple = %+v, want error class sqlstate_23505 with the failure recorded", result)
		}
		state := stateOf(ctx, t, db, "repo-dup-first")
		if state == nil || state.RowCount != nil || state.ContentVersion != nil || state.FailureCount != 1 {
			t.Fatalf("state = %+v, want a failure-only row (NULL row_count and content_version, failure_count 1)", state)
		}
		if got := storedRows(ctx, t, db, "repo-dup-first"); len(got) != 0 {
			t.Fatalf("stored rows = %v, want none", got)
		}
	})
}

// TestDeriveRederiveReplacesRowsWithoutConflictLive: a derive of a repository
// that already has rows deletes them in the same transaction before the
// insert, so deriving again (here at a newer extractor version, the only way
// a second derive of one content version writes) stores the same row set and
// row_count with no key conflict. This is what makes a retried derive
// idempotent; the insert needs no ON CONFLICT clause.
func TestDeriveRederiveReplacesRowsWithoutConflictLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-again", "docker-compose.yml", composeBody(1))
	first := newLiveDeriver(t, db, testExtractorVersion).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-again"})
	if first.Outcome != artifacts.OutcomeOK {
		t.Fatalf("first derive = %+v, want ok", first)
	}
	rowsFirst := storedRows(ctx, t, db, "repo-again")
	if len(rowsFirst) == 0 {
		t.Fatalf("first derive stored no rows")
	}
	second := newLiveDeriver(t, db, testExtractorVersion+1).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-again"})
	if second.Outcome != artifacts.OutcomeOK {
		t.Fatalf("second derive = %+v, want ok", second)
	}
	if got := storedRows(ctx, t, db, "repo-again"); !slices.Equal(got, rowsFirst) {
		t.Fatalf("rows after the second derive = %v, want %v", got, rowsFirst)
	}
	state := stateOf(ctx, t, db, "repo-again")
	if state.RowCount == nil || *state.RowCount != len(rowsFirst) {
		t.Fatalf("row_count = %v, want %d", state.RowCount, len(rowsFirst))
	}
}
