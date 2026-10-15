// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	lockstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
)

// unchangedFanInDB is a Beginner that records every transaction the fan-in
// opens and every repository lock it takes, and answers the base-handle
// readiness check (ListPartitionsMissingReadinessQuery) from a seeded missing
// set. Any other base-handle query fails the test: the fan-in's only
// non-transactional read is the readiness check.
type unchangedFanInDB struct {
	generations  map[string]string
	missing      map[ScopeGenerationPartition]struct{}
	readinessErr error

	mu             sync.Mutex
	begins         int
	lockedRepoKeys []string
	readinessArgs  [][]any
}

func (d *unchangedFanInDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if query != ListPartitionsMissingReadinessQuery {
		return nil, fmt.Errorf("unexpected base-handle query: %s", firstLine(query))
	}
	d.mu.Lock()
	d.readinessArgs = append(d.readinessArgs, args)
	d.mu.Unlock()
	if d.readinessErr != nil {
		return nil, d.readinessErr
	}
	scopes, _ := args[0].(array.StringArray)
	generations, _ := args[1].(array.StringArray)
	rows := &queueFakeRows{}
	for i := range scopes {
		partition := ScopeGenerationPartition{ScopeID: scopes[i], GenerationID: generations[i]}
		if _, ok := d.missing[partition]; ok {
			rows.rows = append(rows.rows, []any{partition.ScopeID, partition.GenerationID})
		}
	}
	return rows, nil
}

func (d *unchangedFanInDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected base-handle exec")
}

func (d *unchangedFanInDB) Begin(context.Context) (db.Transaction, error) {
	d.mu.Lock()
	d.begins++
	d.mu.Unlock()
	return &unchangedFanInTx{database: d}, nil
}

type unchangedFanInTx struct{ database *unchangedFanInDB }

func (tx *unchangedFanInTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.HasPrefix(query, ListArgoCDBearingPartitionsQuery):
		return &queueFakeRows{}, nil
	case query == ActiveScopeGenerationQuery:
		scopeID, _ := args[0].(string)
		return &queueFakeRows{rows: [][]any{{tx.database.generations[scopeID]}}}, nil
	}
	return nil, fmt.Errorf("unexpected tx query: %s", firstLine(query))
}

func (tx *unchangedFanInTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if query == lockstore.DeferredMaintenancePartitionedExclusiveLockSQL {
		key, _ := args[1].(string)
		tx.database.mu.Lock()
		tx.database.lockedRepoKeys = append(tx.database.lockedRepoKeys, key)
		tx.database.mu.Unlock()
	}
	return fakeResult{}, nil
}

func (tx *unchangedFanInTx) Commit() error   { return nil }
func (tx *unchangedFanInTx) Rollback() error { return nil }

// unchangedFanInFixture is three single-repository partitions A, B, C.
func unchangedFanInFixture() (map[ScopeGenerationPartition][]string, map[string]string) {
	contributions := map[ScopeGenerationPartition][]string{
		{ScopeID: "scope-a", GenerationID: "gen-a"}: {"repo-a"},
		{ScopeID: "scope-b", GenerationID: "gen-b"}: {"repo-b"},
		{ScopeID: "scope-c", GenerationID: "gen-c"}: {"repo-c"},
	}
	generations := map[string]string{"scope-a": "gen-a", "scope-b": "gen-b", "scope-c": "gen-c"}
	return contributions, generations
}

func sortedPublishedScopes(phases *recordingPhaseStore) []string {
	phases.mu.Lock()
	defer phases.mu.Unlock()
	scopes := make([]string, 0, len(phases.rows))
	for _, row := range phases.rows {
		if row.Key.Keyspace != reducer.GraphProjectionKeyspaceCrossRepoEvidence ||
			row.Phase != reducer.GraphProjectionPhaseBackwardEvidenceCommitted {
			continue
		}
		scopes = append(scopes, row.Key.ScopeID)
	}
	sort.Strings(scopes)
	return scopes
}

// TestDeferredFanInUnchangedPartitionsOpenNoTransaction pins the #8082
// concurrency contract: an unchanged partition whose readiness row exists
// opens no transaction and takes no lock; an unchanged partition missing its
// readiness row and a changed partition both publish through the normal
// locked transaction. The readiness check is one base-handle query over the
// unchanged candidates only.
func TestDeferredFanInUnchangedPartitionsOpenNoTransaction(t *testing.T) {
	t.Parallel()

	contributions, generations := unchangedFanInFixture()
	database := &unchangedFanInDB{
		generations: generations,
		missing:     map[ScopeGenerationPartition]struct{}{{ScopeID: "scope-b", GenerationID: "gen-b"}: {}},
	}
	store, _, phases := testStore(database)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	reader, instruments := fanInMetricHarness(t)
	unchanged := map[ScopeGenerationPartition]struct{}{
		{ScopeID: "scope-a", GenerationID: "gen-a"}: {},
		{ScopeID: "scope-b", GenerationID: "gen-b"}: {},
	}

	result, err := store.publishChangedDeferredBackfillPartitions(
		context.Background(), contributions, unchanged, nil, "fingerprint", 2, instruments,
	)
	if err != nil {
		t.Fatalf("publishChangedDeferredBackfillPartitions() error = %v", err)
	}

	want := deferredFanInResult{published: 2, skipped: 1, skippedUnchanged: 1, unchangedMissingPhase: 1}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	if database.begins != 2 {
		t.Fatalf("transactions opened = %d, want 2 (B republishes for its missing row, C is changed; A opens none)", database.begins)
	}
	sort.Strings(database.lockedRepoKeys)
	if want := []string{"repo-b", "repo-c"}; !reflect.DeepEqual(database.lockedRepoKeys, want) {
		t.Fatalf("locked repository keys = %v, want %v (an unchanged partition with readiness takes no lock)", database.lockedRepoKeys, want)
	}
	if got, want := sortedPublishedScopes(phases), []string{"scope-b", "scope-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("published readiness scopes = %v, want %v", got, want)
	}
	if len(database.readinessArgs) != 1 {
		t.Fatalf("readiness checks = %d, want exactly 1 batched query", len(database.readinessArgs))
	}
	if got, want := database.readinessArgs[0][0], array.StringArray([]string{"scope-a", "scope-b"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("readiness check scopes = %v, want %v (unchanged candidates only, sorted)", got, want)
	}
	if got := collectFanInCounter(t, reader, "eshu_dp_deferred_backfill_fanin_skipped_total", deferredFanInSkipUnchanged); got != 1 {
		t.Fatalf("fanin_skipped_total{reason=%s} = %d, want 1", deferredFanInSkipUnchanged, got)
	}
	if got := collectFanInCounter(t, reader, "eshu_dp_deferred_backfill_fanin_published_total", ""); got != 2 {
		t.Fatalf("fanin_published_total = %d, want 2", got)
	}
}

// TestPublishDeferredBackfillPartitionsWithoutUnchangedSetPublishesAll pins the
// partition-scoped pass's entry point: no unchanged set, no readiness check,
// every partition publishes, exactly as before #8082.
func TestPublishDeferredBackfillPartitionsWithoutUnchangedSetPublishesAll(t *testing.T) {
	t.Parallel()

	contributions, generations := unchangedFanInFixture()
	database := &unchangedFanInDB{generations: generations}
	store, _, phases := testStore(database)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }

	published, err := store.PublishDeferredBackfillPartitions(context.Background(), contributions, nil, "fingerprint", 2, nil)
	if err != nil {
		t.Fatalf("PublishDeferredBackfillPartitions() error = %v", err)
	}
	if published != 3 || database.begins != 3 {
		t.Fatalf("published = %d, transactions = %d, want 3 and 3", published, database.begins)
	}
	if len(database.readinessArgs) != 0 {
		t.Fatalf("readiness checks = %d, want 0 without an unchanged set", len(database.readinessArgs))
	}
	if got, want := sortedPublishedScopes(phases), []string{"scope-a", "scope-b", "scope-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("published readiness scopes = %v, want %v", got, want)
	}
}

// TestDeferredFanInUnchangedCheckFailurePublishesEveryPartition pins the
// degrade direction: a failed readiness check never skips a partition, it
// falls back to publishing all of them.
func TestDeferredFanInUnchangedCheckFailurePublishesEveryPartition(t *testing.T) {
	t.Parallel()

	contributions, generations := unchangedFanInFixture()
	database := &unchangedFanInDB{generations: generations, readinessErr: errors.New("readiness check boom")}
	store, _, _ := testStore(database)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	unchanged := map[ScopeGenerationPartition]struct{}{
		{ScopeID: "scope-a", GenerationID: "gen-a"}: {},
		{ScopeID: "scope-b", GenerationID: "gen-b"}: {},
	}

	result, err := store.publishChangedDeferredBackfillPartitions(
		context.Background(), contributions, unchanged, nil, "fingerprint", 2, nil,
	)
	if err != nil {
		t.Fatalf("publishChangedDeferredBackfillPartitions() error = %v, want nil (the check is not a correctness dependency)", err)
	}
	if result.published != 3 || result.skippedUnchanged != 0 || database.begins != 3 {
		t.Fatalf("result = %+v, transactions = %d, want 3 published, 0 skipped, 3 transactions", result, database.begins)
	}
}
