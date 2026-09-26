// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lockstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
)

func TestSharedIntentAcceptanceWriterUpsertIntentsUsesTransactionWhenAvailable(t *testing.T) {
	t.Parallel()

	database := newSharedIntentAcceptanceWriterDB()
	writer := postgres.NewSharedIntentAcceptanceWriter(database)
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

	if got, want := database.beginCalls, 1; got != want {
		t.Fatalf("beginCalls = %d, want %d", got, want)
	}
	if database.tx == nil {
		t.Fatal("transaction was not captured")
	}
	if got, want := database.tx.commitCalls, 1; got != want {
		t.Fatalf("commitCalls = %d, want %d", got, want)
	}
	if got, want := database.tx.intentWrites, 1; got != want {
		t.Fatalf("intentWrites = %d, want %d", got, want)
	}
	if got, want := database.tx.acceptanceWrites, 1; got != want {
		t.Fatalf("acceptanceWrites = %d, want %d", got, want)
	}
	if got, want := database.tx.repoLockKeys, []string{"repository:source"}; !slices.Equal(got, want) {
		t.Fatalf("repoLockKeys = %v, want %v", got, want)
	}
	if got, want := database.tx.operations, []string{"lock:repository:source", "intents", "acceptance"}; !slices.Equal(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if got, want := len(database.execs), 0; got != want {
		t.Fatalf("base exec count = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterUpsertIntentsFallsBackWithoutTransactions(t *testing.T) {
	t.Parallel()

	database := &sharedIntentAcceptanceWriterNoTxDB{}
	writer := postgres.NewSharedIntentAcceptanceWriter(database)
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

	if got, want := database.intentWrites, 1; got != want {
		t.Fatalf("intentWrites = %d, want %d", got, want)
	}
	if got, want := database.acceptanceWrites, 1; got != want {
		t.Fatalf("acceptanceWrites = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterRejectsRepoDependencyWithoutTransaction(t *testing.T) {
	t.Parallel()

	database := &sharedIntentAcceptanceWriterNoTxDB{}
	writer := postgres.NewSharedIntentAcceptanceWriter(database)
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
	if got := database.intentWrites; got != 0 {
		t.Fatalf("intentWrites = %d, want 0", got)
	}
	if got := database.acceptanceWrites; got != 0 {
		t.Fatalf("acceptanceWrites = %d, want 0", got)
	}
}

func TestSharedIntentAcceptanceWriterLocksDistinctRepoDependenciesInSortedOrder(t *testing.T) {
	t.Parallel()

	database := newSharedIntentAcceptanceWriterDB()
	writer := postgres.NewSharedIntentAcceptanceWriter(database)
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

	if got, want := database.tx.repoLockKeys, []string{"repository:repo-a", "repository:repo-b"}; !slices.Equal(got, want) {
		t.Fatalf("repoLockKeys = %v, want %v", got, want)
	}
	if got, want := database.tx.operations, []string{
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
	if err := lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(
		context.Background(), holder, []string{"repository:repo-a"},
	); err != nil {
		t.Fatalf("hold repo-a acceptance gate: %v", err)
	}

	database := &sharedIntentAcceptanceWriterLockDB{mgr: mgr}
	writer := postgres.NewSharedIntentAcceptanceWriter(database)
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

	database := newSharedIntentAcceptanceWriterDB()
	writer := postgres.NewSharedIntentAcceptanceWriter(database)

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
	if got, want := database.beginCalls, 0; got != want {
		t.Fatalf("beginCalls = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterUpsertIntentsRejectsMixedGenerationAcceptanceKey(t *testing.T) {
	t.Parallel()

	database := newSharedIntentAcceptanceWriterDB()
	writer := postgres.NewSharedIntentAcceptanceWriter(database)
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
	if got, want := database.beginCalls, 0; got != want {
		t.Fatalf("beginCalls = %d, want %d", got, want)
	}
}

func TestSharedIntentAcceptanceWriterUpsertIntentsRollsBackWhenAcceptanceWriteFails(t *testing.T) {
	t.Parallel()

	database := newSharedIntentAcceptanceWriterDB()
	database.tx = &sharedIntentAcceptanceWriterTx{failAcceptanceWrite: true}
	writer := postgres.NewSharedIntentAcceptanceWriter(database)
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
	if database.tx == nil {
		t.Fatal("transaction was not captured")
	}
	if got, want := database.tx.commitCalls, 0; got != want {
		t.Fatalf("commitCalls = %d, want %d", got, want)
	}
	if got, want := database.tx.rollbackCalls, 1; got != want {
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

func (database *sharedIntentAcceptanceWriterDB) Begin(context.Context) (db.Transaction, error) {
	database.beginCalls++
	if database.tx == nil {
		database.tx = &sharedIntentAcceptanceWriterTx{}
	}
	return database.tx, nil
}

func (database *sharedIntentAcceptanceWriterDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	database.execs = append(database.execs, query)
	return fake.Result{}, nil
}

func (database *sharedIntentAcceptanceWriterDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
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
	case query == lockstore.DeferredMaintenancePartitionedSharedLockSQL:
		repoKey, ok := args[1].(string)
		if !ok {
			return nil, fmt.Errorf("repo lock key = %T, want string", args[1])
		}
		tx.repoLockKeys = append(tx.repoLockKeys, repoKey)
		tx.operations = append(tx.operations, "lock:"+repoKey)
	case strings.Contains(query, "INSERT INTO shared_projection_intents"):
		tx.intentWrites++
		tx.operations = append(tx.operations, "intents")
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
	return fake.Result{}, nil
}

// QueryContext serves the acceptance upsert, which uses RETURNING to report
// the applied keys (#6679).
func (tx *sharedIntentAcceptanceWriterTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if !strings.Contains(query, "INSERT INTO shared_projection_acceptance") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if tx.failAcceptanceWrite {
		return nil, fmt.Errorf("acceptance write failed")
	}
	tx.acceptanceWrites++
	tx.operations = append(tx.operations, "acceptance")
	return acceptanceAppliedRows(args), nil
}

// acceptanceAppliedRows returns every submitted acceptance key as applied,
// matching the RETURNING shape of the advance-only acceptance upsert.
func acceptanceAppliedRows(args []any) *fake.Rows {
	const columnsPerRow = 6
	rows := &fake.Rows{}
	for i := 0; i+columnsPerRow <= len(args); i += columnsPerRow {
		rows.Data = append(rows.Data, []any{args[i], args[i+1], args[i+2]})
	}
	return rows
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

func (database *sharedIntentAcceptanceWriterNoTxDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO shared_projection_intents"):
		database.intentWrites++
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
	return fake.Result{}, nil
}

func (database *sharedIntentAcceptanceWriterNoTxDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	if !strings.Contains(query, "INSERT INTO shared_projection_acceptance") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	database.acceptanceWrites++
	return acceptanceAppliedRows(args), nil
}

// advisoryLockManager copy: this file's lock-ordering proofs need the same
// in-memory advisory-lock simulator that root's
// deferred_maintenance_lock_fakes_test.go defines for the staying deferred
// concurrency proofs. Go test-only symbols do not cross package boundaries,
// so this copy lives with its only other consumer (mirroring the proof-DB
// helper copies the freshness leaves keep per consumer).

// advisoryLockManager simulates Postgres transaction-level advisory lock
// semantics for the deferred-maintenance partition keys: many holders may share
// one key, an exclusive request blocks until no shared or exclusive holder
// remains on that key, and disjoint keys never contend. It lets the concurrency
// proofs run deterministically without a live database.
type advisoryLockManager struct {
	mu        sync.Mutex
	cond      *sync.Cond
	exclusive map[string]bool
	shared    map[string]int
}

func newAdvisoryLockManager() *advisoryLockManager {
	m := &advisoryLockManager{
		exclusive: make(map[string]bool),
		shared:    make(map[string]int),
	}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *advisoryLockManager) acquireExclusive(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.exclusive[key] || m.shared[key] > 0 {
		m.cond.Wait()
	}
	m.exclusive[key] = true
}

func (m *advisoryLockManager) acquireShared(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.exclusive[key] {
		m.cond.Wait()
	}
	m.shared[key]++
}

func (m *advisoryLockManager) release(exclusiveKeys, sharedKeys []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range exclusiveKeys {
		delete(m.exclusive, key)
	}
	for _, key := range sharedKeys {
		if m.shared[key] > 0 {
			m.shared[key]--
		}
	}
	m.cond.Broadcast()
}

// advisoryLockTx is a fake transaction that routes the partitioned advisory lock
// SQL into the simulated lock manager and records the keys it holds so they can
// be released on commit/rollback.
type advisoryLockTx struct {
	mgr           *advisoryLockManager
	exclusiveHeld []string
	sharedHeld    []string
}

func (tx *advisoryLockTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch query {
	case lockstore.DeferredMaintenancePartitionedExclusiveLockSQL:
		key := args[1].(string)
		tx.mgr.acquireExclusive(key)
		tx.exclusiveHeld = append(tx.exclusiveHeld, key)
	case lockstore.DeferredMaintenancePartitionedSharedLockSQL:
		key := args[1].(string)
		tx.mgr.acquireShared(key)
		tx.sharedHeld = append(tx.sharedHeld, key)
	}
	return fake.Result{}, nil
}

func (tx *advisoryLockTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &fake.Rows{}, nil
}

func (tx *advisoryLockTx) Commit() error {
	tx.mgr.release(tx.exclusiveHeld, tx.sharedHeld)
	return nil
}

func (tx *advisoryLockTx) Rollback() error {
	tx.mgr.release(tx.exclusiveHeld, tx.sharedHeld)
	return nil
}

type sharedIntentAcceptanceWriterLockDB struct {
	mgr *advisoryLockManager
}

func (database *sharedIntentAcceptanceWriterLockDB) Begin(context.Context) (db.Transaction, error) {
	return &sharedIntentAcceptanceWriterLockTx{
		advisoryLockTx: &advisoryLockTx{mgr: database.mgr},
	}, nil
}

func (*sharedIntentAcceptanceWriterLockDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected outer exec")
}

func (*sharedIntentAcceptanceWriterLockDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
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
	if query == lockstore.DeferredMaintenancePartitionedSharedLockSQL {
		return tx.advisoryLockTx.ExecContext(ctx, query, args...)
	}
	if strings.Contains(query, "INSERT INTO shared_projection_intents") {
		return fake.Result{}, nil
	}
	return nil, fmt.Errorf("unexpected exec query: %s", query)
}

// QueryContext serves the RETURNING acceptance upsert (#6679).
func (tx *sharedIntentAcceptanceWriterLockTx) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	if strings.Contains(query, "INSERT INTO shared_projection_acceptance") {
		return acceptanceAppliedRows(args), nil
	}
	return nil, fmt.Errorf("unexpected query: %s", query)
}
