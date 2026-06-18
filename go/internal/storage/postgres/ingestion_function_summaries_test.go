package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestIngestionStoreCommitScopeGenerationPersistsFunctionSummariesBeforeProjectorEnqueue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 6, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true
	scopeValue := repositoryScopeFixture()
	generation := repositoryGenerationFixture(now)
	envelopes := []facts.Envelope{repositoryEnvelopeFixture(scopeValue, generation)}
	functionID := summary.NewFunctionID("repo-123", "example.com/repo/pkg", "", "Handle")

	err := store.CommitScopeGenerationWithFunctionSummaries(
		context.Background(),
		scopeValue,
		generation,
		testFactChannel(envelopes),
		[]collector.ValueFlowSummarySnapshot{{
			FunctionID: functionID,
			Effects:    summary.Effects{ParamToReturn: []int{0}},
			Language:   "go",
		}},
	)
	if err != nil {
		t.Fatalf("CommitScopeGenerationWithFunctionSummaries() error = %v, want nil", err)
	}
	if !db.tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
	summaryExec, generationSummaryExec, enqueueExec := -1, -1, -1
	for i, exec := range db.tx.execs {
		if strings.Contains(exec.query, "INSERT INTO function_summaries") {
			summaryExec = i
			if got, want := exec.args[0], string(functionID); got != want {
				t.Fatalf("summary function id arg = %v, want %v", got, want)
			}
			if exec.args[2] == "" {
				t.Fatalf("summary version arg empty in %#v", exec.args)
			}
		}
		if strings.Contains(exec.query, "INSERT INTO function_summary_generations") {
			generationSummaryExec = i
			if got, want := exec.args[0], generation.GenerationID; got != want {
				t.Fatalf("generation summary generation arg = %v, want %v", got, want)
			}
			if got, want := exec.args[1], string(functionID); got != want {
				t.Fatalf("generation summary function id arg = %v, want %v", got, want)
			}
		}
		if strings.Contains(exec.query, "INSERT INTO fact_work_items") {
			enqueueExec = i
		}
	}
	if summaryExec < 0 {
		t.Fatalf("function summary upsert missing from execs: %#v", db.tx.execs)
	}
	if generationSummaryExec < 0 {
		t.Fatalf("function generation summary upsert missing from execs: %#v", db.tx.execs)
	}
	if enqueueExec < 0 {
		t.Fatalf("projector enqueue missing from execs: %#v", db.tx.execs)
	}
	if summaryExec > enqueueExec || generationSummaryExec > enqueueExec {
		t.Fatalf(
			"summary exec index = %d generation summary exec index = %d enqueue exec index = %d; want summaries before enqueue",
			summaryExec,
			generationSummaryExec,
			enqueueExec,
		)
	}
}

func TestIngestionStoreCommitScopeGenerationRollsBackWhenFunctionSummaryPersistenceFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 6, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{execErrors: map[int]error{3: errors.New("summary insert failed")}}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true
	scopeValue := repositoryScopeFixture()
	generation := repositoryGenerationFixture(now)
	envelopes := []facts.Envelope{repositoryEnvelopeFixture(scopeValue, generation)}

	err := store.CommitScopeGenerationWithFunctionSummaries(
		context.Background(),
		scopeValue,
		generation,
		testFactChannel(envelopes),
		[]collector.ValueFlowSummarySnapshot{{
			FunctionID: summary.NewFunctionID("repo-123", "example.com/repo/pkg", "", "Handle"),
			Effects:    summary.Effects{ParamToReturn: []int{0}},
			Language:   "go",
		}},
	)
	if err == nil {
		t.Fatal("CommitScopeGenerationWithFunctionSummaries() error = nil, want summary persistence failure")
	}
	if !strings.Contains(err.Error(), "upsert function summaries") {
		t.Fatalf("CommitScopeGenerationWithFunctionSummaries() error = %v, want summary context", err)
	}
	if db.tx.committed {
		t.Fatal("transaction committed = true, want false")
	}
	if !db.tx.rolledBack {
		t.Fatal("transaction rolledBack = false, want true")
	}
	for _, exec := range db.tx.execs {
		if strings.Contains(exec.query, "INSERT INTO fact_work_items") {
			t.Fatalf("projector enqueue happened after failed summaries: %#v", db.tx.execs)
		}
	}
}

func repositoryScopeFixture() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repo_id": "repo-123",
		},
	}
}

func repositoryGenerationFixture(now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func repositoryEnvelopeFixture(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) facts.Envelope {
	return facts.Envelope{
		FactID:        "fact-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      "repository",
		StableFactKey: "repository:" + scopeValue.PartitionKey,
		ObservedAt:    generation.ObservedAt,
		Payload:       map[string]any{"graph_id": scopeValue.PartitionKey},
		SourceRef: facts.Ref{
			SourceSystem: scopeValue.SourceSystem,
			FactKey:      "fact-key",
		},
	}
}
