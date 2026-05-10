package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestIngestionStoreCommitScopeGenerationPersistsProjectionInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repo_id": "repo-123",
		},
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

	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if !db.tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
	if db.tx.rolledBack {
		t.Fatal("transaction rolledBack = true, want false")
	}
	if got, want := len(db.tx.execs), 4; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	for index, want := range []string{
		"INSERT INTO ingestion_scopes",
		"INSERT INTO scope_generations",
		"INSERT INTO fact_records",
		"INSERT INTO fact_work_items",
	} {
		if !strings.Contains(db.tx.execs[index].query, want) {
			t.Fatalf("exec[%d] query = %q, want substring %q", index, db.tx.execs[index].query, want)
		}
	}
	if got, want := db.tx.execs[3].args[3], "source_local"; got != want {
		t.Fatalf("projector domain arg = %v, want %v", got, want)
	}
}

func TestIngestionStoreCommitScopeGenerationLogsCommitStages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	var logs bytes.Buffer
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true
	store.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

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

	output := logs.String()
	for _, want := range []string{
		`"msg":"ingestion commit stage completed"`,
		`"stage":"begin_transaction"`,
		`"stage":"upsert_facts"`,
		`"fact_count":1`,
		`"batch_count":1`,
		`"stage":"commit_transaction"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("commit stage logs missing %s:\n%s", want, output)
		}
	}
}

func TestIngestionStoreCommitScopeGenerationRollsBackOnProjectorEnqueueFailure(t *testing.T) {
	t.Parallel()

	db := &fakeTransactionalDB{
		tx: &fakeTx{
			execErrors: map[int]error{
				2: errors.New("insert projector work failed"),
			},
		},
	}
	store := NewIngestionStore(db)

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
		IngestedAt:   time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil)
	if err == nil {
		t.Fatal("CommitScopeGeneration() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "enqueue projector work") {
		t.Fatalf("CommitScopeGeneration() error = %q, want enqueue projector work context", err)
	}
	if db.tx.committed {
		t.Fatal("transaction committed = true, want false")
	}
	if !db.tx.rolledBack {
		t.Fatal("transaction rolledBack = false, want true")
	}
}

func TestUpsertIngestionScopeQueryPreservesActiveStatusDuringPendingRefresh(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ingestion_scopes.active_generation_id IS NOT NULL",
		"EXCLUDED.active_generation_id IS NULL",
		"EXCLUDED.status = 'pending'",
		"THEN ingestion_scopes.status",
	} {
		if !strings.Contains(upsertIngestionScopeQuery, want) {
			t.Fatalf("upsertIngestionScopeQuery missing %q", want)
		}
	}
}

func TestListLatestRelationshipFactRecordsQueryQualifiesFactColumns(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listLatestRelationshipFactRecordsQuery, "\n    fact.scope_id,\n") {
		t.Fatalf("listLatestRelationshipFactRecordsQuery must qualify fact.scope_id:\n%s", listLatestRelationshipFactRecordsQuery)
	}
	if !strings.Contains(listLatestRelationshipFactRecordsQuery, "\n    fact.generation_id,\n") {
		t.Fatalf("listLatestRelationshipFactRecordsQuery must qualify fact.generation_id:\n%s", listLatestRelationshipFactRecordsQuery)
	}
}

func TestIngestionStoreCommitClaimedScopeGenerationFencesClaimInTransaction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 10, 30, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-claimed",
		SourceSystem:  "terraform_state",
		ScopeKind:     scope.KindStateSnapshot,
		CollectorKind: scope.CollectorTerraformState,
		PartitionKey:  "tfstate:claimed",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-claimed",
		ScopeID:      "scope-claimed",
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mutation := workflow.ClaimMutation{
		WorkItemID:    "work-item-claimed",
		ClaimID:       "claim-claimed",
		FencingToken:  7,
		OwnerID:       "collector-owner",
		ObservedAt:    now,
		LeaseDuration: time.Minute,
	}

	err := store.CommitClaimedScopeGeneration(
		context.Background(),
		mutation,
		scopeValue,
		generation,
		testFactChannel([]facts.Envelope{{
			FactID:        "fact-claimed",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "terraform_state_snapshot",
			StableFactKey: "snapshot:claimed",
			ObservedAt:    now,
			Payload:       map[string]any{"serial": float64(1)},
		}}),
	)
	if err != nil {
		t.Fatalf("CommitClaimedScopeGeneration() error = %v, want nil", err)
	}
	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if got, want := len(db.tx.execs), 5; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got := db.tx.execs[0].query; !strings.Contains(got, "WITH candidate AS") || !strings.Contains(got, "workflow_claims") || !strings.Contains(got, "status = 'active'") {
		t.Fatalf("first exec query = %q, want active claim fence mutation", got)
	}
	if !db.tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
}

func TestIngestionStoreCommitScopeGenerationSkipsUnchangedActiveGeneration(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-active", "fingerprint-same"}},
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-456",
		ScopeID:       "scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-same",
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got, want := db.beginCalls, 0; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if got, want := len(db.tx.execs), 0; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := telemetry.SkippedRefreshCount(), uint64(1); got != want {
		t.Fatalf("SkippedRefreshCount() = %d, want %d", got, want)
	}
}

func TestIngestionStoreCommitScopeGenerationSkipsUnchangedPendingGeneration(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-pending", "fingerprint-same"}},
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-456",
		ScopeID:       "scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-same",
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"generation.status IN ('pending', 'active')",
		"ORDER BY generation.ingested_at DESC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("freshness query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.beginCalls, 0; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if got, want := telemetry.SkippedRefreshCount(), uint64(1); got != want {
		t.Fatalf("SkippedRefreshCount() = %d, want %d", got, want)
	}
}

func TestIngestionStoreCommitScopeGenerationContinuesWhenActiveFingerprintDiffers(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-active", "fingerprint-old"}},
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-456",
		ScopeID:       "scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-new",
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if got, want := len(db.tx.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := telemetry.SkippedRefreshCount(), uint64(0); got != want {
		t.Fatalf("SkippedRefreshCount() = %d, want %d", got, want)
	}
}

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

	if got, want := len(db.tx.queries), 1; got != want {
		t.Fatalf("transaction query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.tx.queries[0].query, "fact_kind = 'repository'") {
		t.Fatalf("transaction query = %q, want repository catalog load only", db.tx.queries[0].query)
	}
}

func TestIngestionStoreBackfillAllRelationshipEvidenceSkipsUnknownTargetGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			{
				rows: [][]any{
					{"repo-other", "scope-other", "gen-other"},
				},
			},
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
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	for _, execCall := range db.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			t.Fatalf("unexpected evidence insert for unknown target generation:\n%s", execCall.query)
		}
	}
	foundPhasePublish := false
	for _, execCall := range db.execs {
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
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			{
				rows: [][]any{
					{"repo-infra", "scope-infra", "gen-infra"},
					{"repo-app", "scope-app", "gen-app"},
				},
			},
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
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	var evidenceInserts []fakeExecCall
	for _, execCall := range db.execs {
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

type fakeTransactionalDB struct {
	tx             *fakeTx
	beginCalls     int
	beginErr       error
	queries        []fakeQueryCall
	queryResponses []queueFakeRows
}

func (f *fakeTransactionalDB) Begin(context.Context) (Transaction, error) {
	f.beginCalls++
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

func (f *fakeTransactionalDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected ExecContext on outer db")
}

func (f *fakeTransactionalDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	f.queries = append(f.queries, fakeQueryCall{query: query, args: args})
	if len(f.queryResponses) == 0 {
		return nil, errors.New("unexpected QueryContext on outer db")
	}

	rows := f.queryResponses[0]
	f.queryResponses = f.queryResponses[1:]
	if rows.err != nil {
		return nil, rows.err
	}

	return &rows, nil
}

type fakeTx struct {
	execs          []fakeExecCall
	queries        []fakeQueryCall
	execErrors     map[int]error
	queryResponses []queueFakeRows
	committed      bool
	rolledBack     bool
	rollbackHook   func()
}

func (f *fakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	callIndex := len(f.execs)
	f.execs = append(f.execs, fakeExecCall{query: query, args: args})
	if err := f.execErrors[callIndex]; err != nil {
		return nil, err
	}
	return fakeResult{}, nil
}

func (f *fakeTx) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	f.queries = append(f.queries, fakeQueryCall{query: query, args: args})
	if len(f.queryResponses) > 0 {
		rows := f.queryResponses[0]
		f.queryResponses = f.queryResponses[1:]
		if rows.err != nil {
			return nil, rows.err
		}
		return &rows, nil
	}
	if strings.Contains(query, "WITH latest_generations AS") {
		return &queueFakeRows{}, nil
	}
	if strings.Contains(query, "FROM fact_records") && strings.Contains(query, "fact_kind = 'repository'") {
		return &queueFakeRows{}, nil
	}
	return nil, errors.New("unexpected query in transaction")
}

func (f *fakeTx) Commit() error {
	f.committed = true
	return nil
}

func (f *fakeTx) Rollback() error {
	if f.rollbackHook != nil {
		f.rollbackHook()
	}
	f.rolledBack = true
	return nil
}
