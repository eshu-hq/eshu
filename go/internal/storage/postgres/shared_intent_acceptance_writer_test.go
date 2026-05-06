package postgres

import (
	"context"
	"database/sql"
	"fmt"
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
	commitCalls         int
	rollbackCalls       int
	committed           bool
	failAcceptanceWrite bool
}

func (tx *sharedIntentAcceptanceWriterTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO shared_projection_intents"):
		tx.intentWrites++
	case strings.Contains(query, "INSERT INTO shared_projection_acceptance"):
		if tx.failAcceptanceWrite {
			return nil, fmt.Errorf("acceptance write failed")
		}
		tx.acceptanceWrites++
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
