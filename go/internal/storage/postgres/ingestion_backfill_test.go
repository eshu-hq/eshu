package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// backfillTxDB adapts a single fakeExecQueryer into a transactional store so the
// batched deferred backfill (which opens a transaction per repository batch) can
// run against one ordered exec/query log. Begin returns a transaction that
// delegates to the same inner queryer; Commit/Rollback are no-ops. This keeps the
// backfill tests asserting on one execs slice while exercising the per-batch
// transaction path.
type backfillTxDB struct {
	inner      *fakeExecQueryer
	beginCalls int
}

func newBackfillTxDB(inner *fakeExecQueryer) *backfillTxDB {
	return &backfillTxDB{inner: inner}
}

func (db *backfillTxDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.inner.ExecContext(ctx, query, args...)
}

func (db *backfillTxDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return db.inner.QueryContext(ctx, query, args...)
}

func (db *backfillTxDB) Begin(context.Context) (Transaction, error) {
	db.beginCalls++
	return &backfillTx{inner: db.inner}, nil
}

type backfillTx struct {
	inner *fakeExecQueryer
}

func (tx *backfillTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.inner.ExecContext(ctx, query, args...)
}

func (tx *backfillTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.inner.QueryContext(ctx, query, args...)
}

func (tx *backfillTx) Commit() error   { return nil }
func (tx *backfillTx) Rollback() error { return nil }

func TestIngestionStoreCommitScopeGenerationSkipsRelationshipBackfillWhenConfigured(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	envelopes := []facts.Envelope{{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:repo-123",
		ObservedAt:    generation.ObservedAt,
		Payload:       map[string]any{"graph_id": "repo-123"},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			FactKey:      "fact-key",
		},
	}}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testFactChannel(envelopes)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	// Issue #3481: the repository catalog now loads through the shared cache on
	// the store's base connection, not the per-commit transaction. With backfill
	// skipped the transaction issues no reads, and the single base-connection
	// query is the cached catalog load.
	if got, want := len(db.tx.queries), 0; got != want {
		t.Fatalf("transaction query count = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("base connection query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "fact_kind = 'repository'") {
		t.Fatalf("base connection query = %q, want repository catalog load only", db.queries[0].query)
	}
}

func TestIngestionStoreBackfillAllRelationshipEvidenceSkipsUnknownTargetGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	otherGen := [][]any{
		{"repo-other", "scope-other", "gen-other"},
	}
	inner := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// latest relationship facts
			{
				rows: [][]any{
					{
						"fact-1",
						"scope-infra",
						"gen-infra",
						"content",
						"content:1",
						"content.v1",
						"git",
						int64(0),
						"unknown",
						"git",
						"source-fact-1",
						"",
						"",
						now,
						false,
						[]byte(`{"artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`),
					},
				},
			},
			// active repository generations snapshot
			{rows: otherGen},
			// batch transaction re-load of active generations under the lock
			{rows: otherGen},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			t.Fatalf("unexpected evidence insert for unknown target generation:\n%s", execCall.query)
		}
	}
	foundPhasePublish := false
	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO graph_projection_phase_state") {
			foundPhasePublish = true
			break
		}
	}
	if !foundPhasePublish {
		t.Fatal("expected backward evidence readiness publish")
	}
}

func TestIngestionStoreBackfillAllRelationshipEvidencePersistsBySourceGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	inner := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			// latest relationship facts
			{
				rows: [][]any{
					{
						"fact-1",
						"scope-infra",
						"gen-infra",
						"content",
						"content:1",
						"content.v1",
						"git",
						int64(0),
						"unknown",
						"git",
						"source-fact-1",
						"",
						"",
						now,
						false,
						[]byte(`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`),
					},
				},
			},
			// active repository generations snapshot
			{rows: activeGens},
			// batch transaction re-load of active generations under the lock
			{rows: activeGens},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	var evidenceInserts []fakeExecCall
	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			evidenceInserts = append(evidenceInserts, execCall)
		}
	}
	if len(evidenceInserts) != 1 {
		t.Fatalf("relationship evidence inserts = %d, want 1", len(evidenceInserts))
	}
	if got, want := evidenceInserts[0].args[1], "gen-infra"; got != want {
		t.Fatalf("evidence generation_id = %v, want source generation %q", got, want)
	}
}
