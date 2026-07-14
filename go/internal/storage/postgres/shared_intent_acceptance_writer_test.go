// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentAcceptanceWriterUpsertIntentsUsesTransactionWhenAvailable(t *testing.T) {
	t.Parallel()

	db := newSharedIntentAcceptanceWriterDB()
	writer := NewSharedIntentAcceptanceWriter(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-repo-dependency",
			ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey:     "repository:source->repository:target",
			ScopeID:          "scope:git:source",
			AcceptanceUnitID: "repository:source",
			RepositoryID:     "repository:source",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			Payload: map[string]any{
				"target_repository_id": "repository:target",
				"relationship_type":    "DEPENDS_ON",
			},
			CreatedAt: now,
		},
	}

	if err := writer.UpsertIntents(context.Background(), rows); err != nil {
		t.Fatalf("UpsertIntents() error = %v, want nil", err)
	}

	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("beginCalls = %d, want %d", got, want)
	}
	if db.tx == nil {
		t.Fatal("transaction was not captured")
	}
	if got, want := db.tx.commitCalls, 1; got != want {
		t.Fatalf("commitCalls = %d, want %d", got, want)
	}
	if got, want := db.tx.intentWrites, 1; got != want {
		t.Fatalf("intentWrites = %d, want %d", got, want)
	}
	if got, want := db.tx.acceptanceWrites, 1; got != want {
		t.Fatalf("acceptanceWrites = %d, want %d", got, want)
	}
	if got, want := db.tx.repoLockKeys, []string{"repository:source"}; !slices.Equal(got, want) {
		t.Fatalf("repoLockKeys = %v, want %v", got, want)
	}
	if got, want := db.tx.operations, []string{"lock:repository:source", "intents", "acceptance"}; !slices.Equal(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if got, want := len(db.execs), 0; got != want {
		t.Fatalf("base exec count = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterUpsertIntentsFallsBackWithoutTransactions(t *testing.T) {
	t.Parallel()

	db := &sharedIntentAcceptanceWriterNoTxDB{}
	writer := NewSharedIntentAcceptanceWriter(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-repo-dependency",
			ProjectionDomain: reducer.DomainCodeCalls,
			PartitionKey:     "repository:source->repository:target",
			ScopeID:          "scope:git:source",
			AcceptanceUnitID: "repository:source",
			RepositoryID:     "repository:source",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			Payload:          map[string]any{"relationship_type": "DEPENDS_ON"},
			CreatedAt:        now,
		},
	}

	if err := writer.UpsertIntents(context.Background(), rows); err != nil {
		t.Fatalf("UpsertIntents() error = %v, want nil", err)
	}

	if got, want := db.intentWrites, 1; got != want {
		t.Fatalf("intentWrites = %d, want %d", got, want)
	}
	if got, want := db.acceptanceWrites, 1; got != want {
		t.Fatalf("acceptanceWrites = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterRejectsRepoDependencyWithoutTransaction(t *testing.T) {
	t.Parallel()

	db := &sharedIntentAcceptanceWriterNoTxDB{}
	writer := NewSharedIntentAcceptanceWriter(db)
	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-repo-dependency",
			ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey:     "repository:source->repository:target",
			ScopeID:          "scope:git:source",
			AcceptanceUnitID: "repository:source",
			RepositoryID:     "repository:source",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			Payload:          map[string]any{"relationship_type": "DEPENDS_ON"},
			CreatedAt:        time.Now().UTC(),
		},
	}

	err := writer.UpsertIntents(context.Background(), rows)
	if err == nil || !strings.Contains(err.Error(), "requires transactions") {
		t.Fatalf("UpsertIntents() error = %v, want transaction requirement", err)
	}
	if got := db.intentWrites; got != 0 {
		t.Fatalf("intentWrites = %d, want 0", got)
	}
	if got := db.acceptanceWrites; got != 0 {
		t.Fatalf("acceptanceWrites = %d, want 0", got)
	}
}

func TestSharedIntentAcceptanceWriterLocksDistinctRepoDependenciesInSortedOrder(t *testing.T) {
	t.Parallel()

	db := newSharedIntentAcceptanceWriterDB()
	writer := NewSharedIntentAcceptanceWriter(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	rows := []reducer.SharedProjectionIntentRow{
		sharedIntentAcceptanceWriterRow("intent-b", reducer.DomainRepoDependency, "repository:repo-b", now),
		sharedIntentAcceptanceWriterRow("intent-code", reducer.DomainCodeCalls, "repository:repo-code", now),
		sharedIntentAcceptanceWriterRow("intent-a", reducer.DomainRepoDependency, "repository:repo-a", now),
		sharedIntentAcceptanceWriterRow("intent-a-duplicate", reducer.DomainRepoDependency, "repository:repo-a", now),
	}

	if err := writer.UpsertIntents(context.Background(), rows); err != nil {
		t.Fatalf("UpsertIntents() error = %v, want nil", err)
	}

	if got, want := db.tx.repoLockKeys, []string{"repository:repo-a", "repository:repo-b"}; !slices.Equal(got, want) {
		t.Fatalf("repoLockKeys = %v, want %v", got, want)
	}
	if got, want := db.tx.operations, []string{
		"lock:repository:repo-a",
		"lock:repository:repo-b",
		"intents",
		"acceptance",
	}; !slices.Equal(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
}

func TestSharedIntentAcceptanceWriterPreservesDisjointRepoConcurrency(t *testing.T) {
	t.Parallel()

	mgr := newAdvisoryLockManager()
	holder := &advisoryLockTx{mgr: mgr}
	if err := acquireDeferredMaintenanceRepoExclusiveLocks(
		context.Background(), holder, []string{"repository:repo-a"},
	); err != nil {
		t.Fatalf("hold repo-a acceptance gate: %v", err)
	}

	db := &sharedIntentAcceptanceWriterLockDB{mgr: mgr}
	writer := NewSharedIntentAcceptanceWriter(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	sameRepoDone := make(chan error, 1)
	go func() {
		sameRepoDone <- writer.UpsertIntents(context.Background(), []reducer.SharedProjectionIntentRow{
			sharedIntentAcceptanceWriterRow("intent-a", reducer.DomainRepoDependency, "repository:repo-a", now),
		})
	}()

	select {
	case err := <-sameRepoDone:
		t.Fatalf("same-repo acceptance completed before gate release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	disjointCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := writer.UpsertIntents(disjointCtx, []reducer.SharedProjectionIntentRow{
		sharedIntentAcceptanceWriterRow("intent-b", reducer.DomainRepoDependency, "repository:repo-b", now),
	}); err != nil {
		t.Fatalf("disjoint repo acceptance blocked: %v", err)
	}

	if err := holder.Commit(); err != nil {
		t.Fatalf("release repo-a acceptance gate: %v", err)
	}
	select {
	case err := <-sameRepoDone:
		if err != nil {
			t.Fatalf("same-repo acceptance after gate release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("same-repo acceptance did not proceed after gate release")
	}
}

func sharedIntentAcceptanceWriterRow(
	intentID string,
	domain string,
	acceptanceUnitID string,
	createdAt time.Time,
) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: domain,
		PartitionKey:     intentID,
		ScopeID:          "scope:" + intentID,
		AcceptanceUnitID: acceptanceUnitID,
		RepositoryID:     acceptanceUnitID,
		SourceRunID:      "run-001",
		GenerationID:     "gen-001",
		Payload:          map[string]any{"relationship_type": "DEPENDS_ON"},
		CreatedAt:        createdAt,
	}
}

func TestSharedIntentAcceptanceWriterUpsertIntentsRejectsMissingAcceptanceIdentity(t *testing.T) {
	t.Parallel()

	db := newSharedIntentAcceptanceWriterDB()
	writer := NewSharedIntentAcceptanceWriter(db)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-missing-identity",
			ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey:     "repository:source->repository:target",
			RepositoryID:     "repository:source",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			Payload:          map[string]any{"relationship_type": "DEPENDS_ON"},
		},
	}

	err := writer.UpsertIntents(context.Background(), rows)
	if err == nil {
		t.Fatal("UpsertIntents() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing acceptance identity") {
		t.Fatalf("UpsertIntents() error = %v, want missing acceptance identity", err)
	}
	if got, want := db.beginCalls, 0; got != want {
		t.Fatalf("beginCalls = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterUpsertIntentsRejectsMixedGenerationAcceptanceKey(t *testing.T) {
	t.Parallel()

	db := newSharedIntentAcceptanceWriterDB()
	writer := NewSharedIntentAcceptanceWriter(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-1",
			ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey:     "repository:source->repository:target-a",
			ScopeID:          "scope:git:source",
			AcceptanceUnitID: "repository:source",
			RepositoryID:     "repository:source",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			Payload:          map[string]any{"relationship_type": "DEPENDS_ON"},
			CreatedAt:        now,
		},
		{
			IntentID:         "intent-2",
			ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey:     "repository:source->repository:target-b",
			ScopeID:          "scope:git:source",
			AcceptanceUnitID: "repository:source",
			RepositoryID:     "repository:source",
			SourceRunID:      "run-001",
			GenerationID:     "gen-002",
			Payload:          map[string]any{"relationship_type": "DEPENDS_ON"},
			CreatedAt:        now.Add(time.Second),
		},
	}

	err := writer.UpsertIntents(context.Background(), rows)
	if err == nil {
		t.Fatal("UpsertIntents() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "mixed generations") {
		t.Fatalf("UpsertIntents() error = %v, want mixed generations", err)
	}
	if got, want := db.beginCalls, 0; got != want {
		t.Fatalf("beginCalls = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterUpsertIntentsRollsBackWhenAcceptanceWriteFails(t *testing.T) {
	t.Parallel()

	db := newSharedIntentAcceptanceWriterDB()
	db.tx = &sharedIntentAcceptanceWriterTx{failAcceptanceWrite: true}
	writer := NewSharedIntentAcceptanceWriter(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-repo-dependency",
			ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey:     "repository:source->repository:target",
			ScopeID:          "scope:git:source",
			AcceptanceUnitID: "repository:source",
			RepositoryID:     "repository:source",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			Payload:          map[string]any{"relationship_type": "DEPENDS_ON"},
			CreatedAt:        now,
		},
	}

	err := writer.UpsertIntents(context.Background(), rows)
	if err == nil {
		t.Fatal("UpsertIntents() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "upsert shared projection acceptance") {
		t.Fatalf("UpsertIntents() error = %v, want shared projection acceptance failure", err)
	}
	if db.tx == nil {
		t.Fatal("transaction was not captured")
	}
	if got, want := db.tx.commitCalls, 0; got != want {
		t.Fatalf("commitCalls = %d, want %d", got, want)
	}
	if got, want := db.tx.rollbackCalls, 1; got != want {
		t.Fatalf("rollbackCalls = %d, want %d", got, want)
	}
}

type sharedIntentAcceptanceWriterDB struct {
	beginCalls int
	tx         *sharedIntentAcceptanceWriterTx
	execs      []string
}

func newSharedIntentAcceptanceWriterDB() *sharedIntentAcceptanceWriterDB {
	return &sharedIntentAcceptanceWriterDB{}
}

func (db *sharedIntentAcceptanceWriterDB) Begin(context.Context) (Transaction, error) {
	db.beginCalls++
	if db.tx == nil {
		db.tx = &sharedIntentAcceptanceWriterTx{}
	}
	return db.tx, nil
}

func (db *sharedIntentAcceptanceWriterDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	db.execs = append(db.execs, query)
	return sharedIntentResult{}, nil
}

func (db *sharedIntentAcceptanceWriterDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, fmt.Errorf("unexpected query")
}

type sharedIntentAcceptanceWriterTx struct {
	intentWrites        int
	acceptanceWrites    int
	repoLockKeys        []string
	operations          []string
	commitCalls         int
	rollbackCalls       int
	committed           bool
	failAcceptanceWrite bool
}

func (tx *sharedIntentAcceptanceWriterTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case query == deferredMaintenancePartitionedSharedLockSQL:
		repoKey, ok := args[1].(string)
		if !ok {
			return nil, fmt.Errorf("repo lock key = %T, want string", args[1])
		}
		tx.repoLockKeys = append(tx.repoLockKeys, repoKey)
		tx.operations = append(tx.operations, "lock:"+repoKey)
	case strings.Contains(query, "INSERT INTO shared_projection_intents"):
		tx.intentWrites++
		tx.operations = append(tx.operations, "intents")
	case strings.Contains(query, "INSERT INTO shared_projection_acceptance"):
		if tx.failAcceptanceWrite {
			return nil, fmt.Errorf("acceptance write failed")
		}
		tx.acceptanceWrites++
		tx.operations = append(tx.operations, "acceptance")
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
	return sharedIntentResult{}, nil
}

func (tx *sharedIntentAcceptanceWriterTx) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, fmt.Errorf("unexpected query")
}

func (tx *sharedIntentAcceptanceWriterTx) Commit() error {
	tx.commitCalls++
	tx.committed = true
	return nil
}

func (tx *sharedIntentAcceptanceWriterTx) Rollback() error {
	if tx.committed {
		return nil
	}
	tx.rollbackCalls++
	return nil
}

type sharedIntentAcceptanceWriterNoTxDB struct {
	intentWrites     int
	acceptanceWrites int
}

func (db *sharedIntentAcceptanceWriterNoTxDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO shared_projection_intents"):
		db.intentWrites++
	case strings.Contains(query, "INSERT INTO shared_projection_acceptance"):
		db.acceptanceWrites++
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
	return sharedIntentResult{}, nil
}

func (db *sharedIntentAcceptanceWriterNoTxDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, fmt.Errorf("unexpected query")
}

type sharedIntentAcceptanceWriterLockDB struct {
	mgr *advisoryLockManager
}

func (db *sharedIntentAcceptanceWriterLockDB) Begin(context.Context) (Transaction, error) {
	return &sharedIntentAcceptanceWriterLockTx{
		advisoryLockTx: &advisoryLockTx{mgr: db.mgr},
	}, nil
}

func (*sharedIntentAcceptanceWriterLockDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected outer exec")
}

func (*sharedIntentAcceptanceWriterLockDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, fmt.Errorf("unexpected outer query")
}

type sharedIntentAcceptanceWriterLockTx struct {
	*advisoryLockTx
}

func (tx *sharedIntentAcceptanceWriterLockTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	if query == deferredMaintenancePartitionedSharedLockSQL {
		return tx.advisoryLockTx.ExecContext(ctx, query, args...)
	}
	if strings.Contains(query, "INSERT INTO shared_projection_intents") ||
		strings.Contains(query, "INSERT INTO shared_projection_acceptance") {
		return sharedIntentResult{}, nil
	}
	return nil, fmt.Errorf("unexpected exec query: %s", query)
}
