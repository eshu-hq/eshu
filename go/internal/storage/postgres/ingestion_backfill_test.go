package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

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
