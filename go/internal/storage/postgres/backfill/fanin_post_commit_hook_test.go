// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// hookPhaseStore is a PhaseStore that holds side effects until its
// transaction commits and reports the commit through PostCommitHook.
type hookPhaseStore struct {
	database *postCommitWakeDB
	scopeID  string
}

func (h *hookPhaseStore) PublishGraphProjectionPhases(_ context.Context, rows []gpphase.PhaseState) error {
	h.scopeID = rows[0].Key.ScopeID
	return nil
}

func (h *hookPhaseStore) AfterCommit(context.Context) { h.database.log("hook:" + h.scopeID) }

// TestFanInPostCommitHookRunsAfterCommitOnly pins the #11624 contract: a
// phase store that implements PostCommitHook is told about the commit exactly
// once, strictly after it, and never for a partition whose commit failed or
// that was skipped, so deferred requeue counts of a rolled-back transaction
// are dropped.
func TestFanInPostCommitHookRunsAfterCommitOnly(t *testing.T) {
	t.Parallel()

	database := &postCommitWakeDB{
		generations: map[string]string{"scope-a": "gen-a", "scope-b": "gen-b-newer", "scope-c": "gen-c"},
		commitErr:   map[string]error{"scope-c": errors.New("commit refused")},
	}
	store, _, _ := testStore(database)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.NewPhaseStore = func(db.Transaction) PhaseStore { return &hookPhaseStore{database: database} }

	contributions := map[ScopeGenerationPartition][]string{
		{ScopeID: "scope-a", GenerationID: "gen-a"}: {"repo-a"},
		{ScopeID: "scope-b", GenerationID: "gen-b"}: {"repo-b"},
	}
	if _, err := store.PublishDeferredBackfillPartitions(context.Background(), contributions, nil, "", 1, nil); err != nil {
		t.Fatalf("PublishDeferredBackfillPartitions() error = %v", err)
	}
	want := []string{"commit:scope-a", "hook:scope-a", "rollback:scope-b"}
	if !reflect.DeepEqual(database.events, want) {
		t.Fatalf("events = %v, want %v (hook strictly after commit, none for the skip)", database.events, want)
	}

	database.events = nil
	_, err := store.PublishDeferredBackfillPartitions(context.Background(),
		map[ScopeGenerationPartition][]string{{ScopeID: "scope-c", GenerationID: "gen-c"}: {"repo-c"}},
		nil, "", 1, nil)
	if err == nil {
		t.Fatal("PublishDeferredBackfillPartitions() error = nil, want the commit failure")
	}
	for _, event := range database.events {
		if event == "hook:scope-c" {
			t.Fatalf("events = %v, want no hook after a failed commit", database.events)
		}
	}
}
