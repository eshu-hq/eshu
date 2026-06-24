// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestCollectorGenerationDeadLetterSchemaRegistered(t *testing.T) {
	t.Parallel()

	var sql string
	for _, def := range BootstrapDefinitions() {
		if def.Name == "collector_generation_dead_letters" {
			sql = def.SQL
			break
		}
	}
	if strings.TrimSpace(sql) == "" {
		t.Fatal("collector_generation_dead_letters schema definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS collector_generation_dead_letters",
		"generation_id TEXT NOT NULL",
		"payload_reference JSONB NOT NULL DEFAULT '{}'::jsonb",
		"failure_class TEXT NOT NULL",
		"status TEXT NOT NULL",
		"last_replayed_at TIMESTAMPTZ NULL",
		"replayed_generation_id TEXT NOT NULL DEFAULT ''",
		"collector_generation_dead_letters_status_idx",
		"collector_generation_dead_letters_scope_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("collector generation dead-letter schema missing %q:\n%s", want, sql)
		}
	}
}

func TestCollectorGenerationDeadLetterStatusQueryCountsUnresolvedRows(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"SUM(replay_count) FILTER (WHERE status IN ('dead_letter', 'replay_requested'))",
		"MIN(last_dead_lettered_at) FILTER (WHERE status IN ('dead_letter', 'replay_requested'))",
	} {
		if !strings.Contains(collectorGenerationDeadLetterStatusQuery, want) {
			t.Fatalf("status query missing unresolved-row aggregate %q:\n%s", want, collectorGenerationDeadLetterStatusQuery)
		}
	}
}

func TestCollectorGenerationDeadLetterStoreCompletesReplayAfterSuccessfulCommit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 18, 45, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewCollectorGenerationDeadLetterStore(db)

	err := store.CompleteGenerationDeadLetterReplay(context.Background(), collector.GenerationDeadLetterReplayCompletion{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-replayed",
			ScopeID:      "scope-123",
			ObservedAt:   now,
			IngestedAt:   now,
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
		CompletedAt: now,
	})
	if err != nil {
		t.Fatalf("CompleteGenerationDeadLetterReplay() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE collector_generation_dead_letters",
		"status = 'replayed'",
		"replayed_generation_id = $6",
		"status IN ('dead_letter', 'replay_requested')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("complete replay query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[1], "scope-123"; got != want {
		t.Fatalf("scope_id arg = %v, want %q", got, want)
	}
	if got, want := db.execs[0].args[5], "generation-replayed"; got != want {
		t.Fatalf("replayed_generation_id arg = %v, want %q", got, want)
	}
}

func TestCollectorGenerationDeadLetterStoreRecordsGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 18, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewCollectorGenerationDeadLetterStore(db)

	err := store.RecordGenerationDeadLetter(context.Background(), collector.GenerationDeadLetter{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
			ScopeID:      "scope-123",
			ObservedAt:   now.Add(-time.Minute),
			IngestedAt:   now,
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
		FailureClass:     "commit_failure",
		FailureMessage:   "commit scope generation: enqueue projector work: insert failed",
		PayloadReference: map[string]string{"partition_key": "repo-123"},
		DeadLetteredAt:   now,
	})
	if err != nil {
		t.Fatalf("RecordGenerationDeadLetter() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	if !strings.Contains(exec.query, "INSERT INTO collector_generation_dead_letters") {
		t.Fatalf("record query missing dead-letter insert:\n%s", exec.query)
	}
	if !strings.Contains(exec.query, "ON CONFLICT (generation_id)") {
		t.Fatalf("record query must be idempotent by generation_id:\n%s", exec.query)
	}
	if got, want := exec.args[1], "scope-123"; got != want {
		t.Fatalf("scope_id arg = %v, want %q", got, want)
	}
	if got, want := exec.args[2], "generation-456"; got != want {
		t.Fatalf("generation_id arg = %v, want %q", got, want)
	}
	if got, want := exec.args[9], "commit_failure"; got != want {
		t.Fatalf("failure_class arg = %v, want %q", got, want)
	}
}

func TestCollectorGenerationDeadLetterStoreReplayRequestsDeadLetteredGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 18, 15, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-456"}},
		}},
	}
	store := NewCollectorGenerationDeadLetterStore(db)

	result, err := store.ReplayGenerationDeadLetters(context.Background(), collector.GenerationDeadLetterReplayFilter{
		ScopeIDs:      []string{"scope-123"},
		FailureClass:  "commit_failure",
		CollectorKind: scope.CollectorGit,
		Limit:         10,
	}, now)
	if err != nil {
		t.Fatalf("ReplayGenerationDeadLetters() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 1; got != want {
		t.Fatalf("replayed count = %d, want %d", got, want)
	}
	if len(result.GenerationIDs) != 1 || result.GenerationIDs[0] != "generation-456" {
		t.Fatalf("replayed generation IDs = %v, want [generation-456]", result.GenerationIDs)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"UPDATE collector_generation_dead_letters",
		"status = 'replay_requested'",
		"status = 'dead_letter'",
		"scope_id = ANY",
		"failure_class =",
		"collector_kind =",
		"RETURNING generation_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("replay query missing %q:\n%s", want, query)
		}
	}
}
