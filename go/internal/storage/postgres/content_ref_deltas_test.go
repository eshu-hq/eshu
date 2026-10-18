// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- UpsertRefDelta hash-gating tests (#5393 Phase 1) ---

func TestRefDeltaStoreUpsertWritesDivergedRow(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)
	store.Now = func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }

	delta := RefFileDelta{
		RepoID:       "repo-1",
		Ref:          "feature-x",
		RelativePath: "main.go",
		Content:      "package main // diverged\n",
		ContentHash:  "branch-hash",
	}

	if err := store.UpsertRefDelta(context.Background(), delta, "default-hash"); err != nil {
		t.Fatalf("UpsertRefDelta() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	if !strings.Contains(exec.query, "INSERT INTO content_file_ref_deltas") {
		t.Fatalf("query = %q, want content_file_ref_deltas insert", exec.query)
	}
	if err := assertRefDeltaStatementsNeverWriteCanonical([]string{exec.query}); err != nil {
		t.Fatalf("executed statement writes canonical content_files: %v", err)
	}
	wantArgs := []any{"repo-1", "feature-x", "main.go", "package main // diverged\n", "branch-hash", false}
	if len(exec.args) != len(wantArgs)+1 {
		t.Fatalf("arg count = %d, want %d (row fields plus indexed_at)", len(exec.args), len(wantArgs)+1)
	}
	for i, want := range wantArgs {
		if exec.args[i] != want {
			t.Fatalf("args[%d] = %v, want %v", i, exec.args[i], want)
		}
	}
}

func TestRefDeltaStoreUpsertConvergesMatchingHashToDelete(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	// Branch content identical to default: no delta row may survive, so the
	// write converges to a delete instead of an upsert (#5393 hash-gating).
	delta := RefFileDelta{
		RepoID:       "repo-1",
		Ref:          "feature-x",
		RelativePath: "main.go",
		Content:      "package main\n",
		ContentHash:  "same-hash",
	}

	if err := store.UpsertRefDelta(context.Background(), delta, "same-hash"); err != nil {
		t.Fatalf("UpsertRefDelta() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	if !strings.Contains(exec.query, "DELETE FROM content_file_ref_deltas") {
		t.Fatalf("query = %q, want converged delete of the delta row", exec.query)
	}
	if strings.Contains(exec.query, "content_files") {
		t.Fatalf("query = %q, must not touch canonical content_files", exec.query)
	}
}

func TestRefDeltaStoreUpsertDeletedWritesTombstone(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	delta := RefFileDelta{
		RepoID:       "repo-1",
		Ref:          "feature-x",
		RelativePath: "gone.go",
		ContentHash:  "default-hash-at-delete",
		Deleted:      true,
	}

	if err := store.UpsertRefDelta(context.Background(), delta, "default-hash-at-delete"); err != nil {
		t.Fatalf("UpsertRefDelta() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	if !strings.Contains(exec.query, "INSERT INTO content_file_ref_deltas") {
		t.Fatalf("query = %q, want tombstone upsert", exec.query)
	}
	// content binds NULL (tombstone shape), is_deleted binds true.
	if exec.args[3] != nil {
		t.Fatalf("content arg = %v, want nil for a tombstone", exec.args[3])
	}
	if exec.args[5] != true {
		t.Fatalf("is_deleted arg = %v, want true", exec.args[5])
	}
}

func TestRefDeltaStoreUpsertRejectsBlankKeys(t *testing.T) {
	t.Parallel()

	cases := map[string]RefFileDelta{
		"blank repo": {Ref: "r", RelativePath: "p", Content: "c", ContentHash: "h"},
		"blank ref":  {RepoID: "repo", RelativePath: "p", Content: "c", ContentHash: "h"},
		"blank path": {RepoID: "repo", Ref: "r", Content: "c", ContentHash: "h"},
		"blank hash": {RepoID: "repo", Ref: "r", RelativePath: "p", Content: "c"},
	}
	for name, delta := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := &fakeExecQueryer{}
			store := NewRefDeltaStore(db)
			if err := store.UpsertRefDelta(context.Background(), delta, "default-hash"); err == nil {
				t.Fatal("UpsertRefDelta() error = nil, want non-nil")
			}
			if len(db.execs) != 0 {
				t.Fatalf("exec count = %d, want 0 (fail closed, no write)", len(db.execs))
			}
		})
	}
}

func TestRefDeltaStoreUpsertRejectsMissingDefaultHash(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	delta := RefFileDelta{
		RepoID: "repo-1", Ref: "feature-x", RelativePath: "main.go",
		Content: "x", ContentHash: "h",
	}
	// Without the default hash the gate cannot decide; writing anyway would
	// risk shadowing canonical content, so it fails closed.
	if err := store.UpsertRefDelta(context.Background(), delta, "   "); err == nil {
		t.Fatal("UpsertRefDelta() error = nil, want non-nil for blank default hash")
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0", len(db.execs))
	}
}

// --- Tombstone + retract tests ---

func TestRefDeltaStoreWriteTombstone(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	if err := store.WriteRefTombstone(context.Background(), "repo-1", "feature-x", "gone.go", "default-hash"); err != nil {
		t.Fatalf("WriteRefTombstone() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	if !strings.Contains(exec.query, "INSERT INTO content_file_ref_deltas") {
		t.Fatalf("query = %q, want tombstone upsert", exec.query)
	}
	if exec.args[3] != nil || exec.args[5] != true {
		t.Fatalf("tombstone args = %v, want NULL content and is_deleted true", exec.args)
	}
}

func TestRefDeltaStoreWriteTombstoneRejectsBlankKeys(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	if err := store.WriteRefTombstone(context.Background(), "", "feature-x", "gone.go", "h"); err == nil {
		t.Fatal("WriteRefTombstone() error = nil, want non-nil")
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0", len(db.execs))
	}
}

func TestRefDeltaStoreDeleteRefDeltaRemovesOnePath(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	if err := store.DeleteRefDelta(context.Background(), "repo-1", "feature-x", "main.go"); err != nil {
		t.Fatalf("DeleteRefDelta() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	for _, want := range []string{"DELETE FROM content_file_ref_deltas", "repo_id = $1", "ref = $2", "relative_path = $3"} {
		if !strings.Contains(exec.query, want) {
			t.Fatalf("query = %q, want %q", exec.query, want)
		}
	}
}

func TestRefDeltaStoreDeleteRefDeltasRetractsWholeOverlay(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	// Overlay retraction always carries the ref in its key (#5393 invariant
	// 2): one statement removes the ref's rows and nothing else's.
	if err := store.DeleteRefDeltas(context.Background(), "repo-1", "feature-x"); err != nil {
		t.Fatalf("DeleteRefDeltas() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	for _, want := range []string{"DELETE FROM content_file_ref_deltas", "repo_id = $1", "ref = $2"} {
		if !strings.Contains(exec.query, want) {
			t.Fatalf("query = %q, want %q", exec.query, want)
		}
	}
	if strings.Contains(exec.query, "relative_path") {
		t.Fatalf("query = %q, whole-overlay retract must not narrow to a path", exec.query)
	}
	if len(exec.args) != 2 {
		t.Fatalf("arg count = %d, want 2 (repo_id, ref)", len(exec.args))
	}
}

// --- base_default_sha + bounded re-diff trigger tests ---

func TestRefDeltaStoreRecordDeltaBase(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	if err := store.RecordDeltaBase(context.Background(), "repo-1", "feature-x", "base-sha"); err != nil {
		t.Fatalf("RecordDeltaBase() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO content_file_ref_delta_bases") {
		t.Fatalf("query = %q, want delta-bases upsert", db.execs[0].query)
	}
}

func TestRefDeltaStoreDeltaBaseStaleWhenDefaultAdvanced(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"old-base-sha"}}},
		},
	}
	store := NewRefDeltaStore(db)

	stale, err := store.DeltaBaseStale(context.Background(), "repo-1", "feature-x", "new-default-sha")
	if err != nil {
		t.Fatalf("DeltaBaseStale() error = %v, want nil", err)
	}
	if !stale {
		t.Fatal("DeltaBaseStale() = false, want true: the default advanced past the recorded base")
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d (one bounded PK lookup)", got, want)
	}
}

func TestRefDeltaStoreDeltaBaseFreshWhenEqual(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"same-sha"}}},
		},
	}
	store := NewRefDeltaStore(db)

	stale, err := store.DeltaBaseStale(context.Background(), "repo-1", "feature-x", "same-sha")
	if err != nil {
		t.Fatalf("DeltaBaseStale() error = %v, want nil", err)
	}
	if stale {
		t.Fatal("DeltaBaseStale() = true, want false: base matches the current default")
	}
}

func TestRefDeltaStoreDeltaBaseStaleWhenNeverRecorded(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewRefDeltaStore(db)

	// No base row means the ref was never diffed: report stale so the caller
	// schedules the bounded re-diff instead of trusting absent deltas.
	stale, err := store.DeltaBaseStale(context.Background(), "repo-1", "feature-x", "any-sha")
	if err != nil {
		t.Fatalf("DeltaBaseStale() error = %v, want nil", err)
	}
	if !stale {
		t.Fatal("DeltaBaseStale() = false, want true for a never-recorded base")
	}
}

func TestRefDeltaStoreDeltaBasePropagatesQueryError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("db down")}},
	}
	store := NewRefDeltaStore(db)

	_, err := store.DeltaBaseStale(context.Background(), "repo-1", "feature-x", "sha")
	if err == nil {
		t.Fatal("DeltaBaseStale() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "delta base") {
		t.Fatalf("error = %q, want 'delta base' context", err)
	}
}

func TestRefDeltaStoreClearDeltaBase(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewRefDeltaStore(db)

	if err := store.ClearDeltaBase(context.Background(), "repo-1", "feature-x"); err != nil {
		t.Fatalf("ClearDeltaBase() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_file_ref_delta_bases") {
		t.Fatalf("query = %q, want delta-bases delete", db.execs[0].query)
	}
}

func TestRefDeltaStoreRequiresDB(t *testing.T) {
	t.Parallel()

	store := RefDeltaStore{}

	if err := store.UpsertRefDelta(context.Background(), RefFileDelta{
		RepoID: "r", Ref: "b", RelativePath: "p", Content: "c", ContentHash: "h",
	}, "d"); err == nil {
		t.Fatal("UpsertRefDelta() error = nil, want non-nil without a database")
	}
	if _, err := store.DeltaBaseStale(context.Background(), "r", "b", "s"); err == nil {
		t.Fatal("DeltaBaseStale() error = nil, want non-nil without a database")
	}
}
