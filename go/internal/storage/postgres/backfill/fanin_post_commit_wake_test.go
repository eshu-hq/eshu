// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// postCommitWakeDB is a Beginner whose transactions answer the fan-in's
// generation re-read and log commit and rollback events, so the #11104 proof
// can order the post-commit wake against the publishing commit.
type postCommitWakeDB struct {
	generations map[string]string
	commitErr   map[string]error

	mu     sync.Mutex
	events []string
}

func (d *postCommitWakeDB) log(event string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, event)
}

func (d *postCommitWakeDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	return nil, fmt.Errorf("unexpected base-handle query: %s", firstLine(query))
}

func (d *postCommitWakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected base-handle exec")
}

func (d *postCommitWakeDB) Begin(context.Context) (db.Transaction, error) {
	return &postCommitWakeTx{database: d}, nil
}

type postCommitWakeTx struct {
	database *postCommitWakeDB
	scopeID  string
}

func (tx *postCommitWakeTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if query == ActiveScopeGenerationQuery {
		tx.scopeID, _ = args[0].(string)
		return &queueFakeRows{rows: [][]any{{tx.database.generations[tx.scopeID]}}}, nil
	}
	return nil, fmt.Errorf("unexpected tx query: %s", firstLine(query))
}

func (tx *postCommitWakeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (tx *postCommitWakeTx) Commit() error {
	if err := tx.database.commitErr[tx.scopeID]; err != nil {
		return err
	}
	tx.database.log("commit:" + tx.scopeID)
	return nil
}

func (tx *postCommitWakeTx) Rollback() error {
	tx.database.log("rollback:" + tx.scopeID)
	return nil
}

// TestFanInPostCommitWakeRunsAfterCommitOnly pins the #11104 seam contract:
// the post-commit re-wake runs once per published partition, strictly after
// that partition's commit, with exactly the readiness row it published; a
// partition skipped because its generation advanced, and one whose commit
// failed, never re-wake.
func TestFanInPostCommitWakeRunsAfterCommitOnly(t *testing.T) {
	t.Parallel()

	database := &postCommitWakeDB{
		generations: map[string]string{"scope-a": "gen-a", "scope-b": "gen-b-newer", "scope-c": "gen-c"},
		commitErr:   map[string]error{"scope-c": errors.New("commit refused")},
	}
	store, _, _ := testStore(database)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	var woken [][]gpphase.PhaseState
	store.WakePhaseWaitsAfterCommit = func(_ context.Context, rows []gpphase.PhaseState) {
		database.log("wake:" + rows[0].Key.ScopeID)
		woken = append(woken, rows)
	}

	contributions := map[ScopeGenerationPartition][]string{
		{ScopeID: "scope-a", GenerationID: "gen-a"}: {"repo-a"},
		{ScopeID: "scope-b", GenerationID: "gen-b"}: {"repo-b"},
	}
	published, err := store.PublishDeferredBackfillPartitions(context.Background(), contributions, nil, "", 1, nil)
	if err != nil {
		t.Fatalf("PublishDeferredBackfillPartitions() error = %v", err)
	}
	if published != 1 {
		t.Fatalf("published = %d, want 1 (scope-b's generation advanced)", published)
	}
	if got, want := database.events, []string{"commit:scope-a", "wake:scope-a", "rollback:scope-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v (wake strictly after commit, none for the skip)", got, want)
	}
	wantRow := gpphase.PhaseState{
		Key: gpphase.PhaseKey{
			ScopeID: "scope-a", AcceptanceUnitID: "scope-a", SourceRunID: "gen-a",
			GenerationID: "gen-a", Keyspace: gpphase.KeyspaceCrossRepoEvidence,
		},
		Phase:       gpphase.PhaseBackwardEvidenceCommitted,
		CommittedAt: time.Unix(0, 0).UTC(),
		UpdatedAt:   time.Unix(0, 0).UTC(),
	}
	if len(woken) != 1 || !reflect.DeepEqual(woken[0], []gpphase.PhaseState{wantRow}) {
		t.Fatalf("woken rows = %+v, want exactly the published row %+v", woken, wantRow)
	}

	// A failed commit is an error and never re-wakes.
	database.events = nil
	_, err = store.PublishDeferredBackfillPartitions(context.Background(),
		map[ScopeGenerationPartition][]string{{ScopeID: "scope-c", GenerationID: "gen-c"}: {"repo-c"}},
		nil, "", 1, nil)
	if err == nil {
		t.Fatal("PublishDeferredBackfillPartitions() error = nil, want the commit failure")
	}
	for _, event := range database.events {
		if event == "wake:scope-c" {
			t.Fatalf("events = %v, want no wake after a failed commit", database.events)
		}
	}
}
