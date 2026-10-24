// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func stateWriterFixture(now time.Time) (scope.IngestionScope, scope.ScopeGeneration, workflow.ClaimMutation) {
	scopeValue := scope.IngestionScope{
		ScopeID:       "security-alert:github:org:acme",
		SourceSystem:  "security_alert",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorSecurityAlert,
		PartitionKey:  "org:acme",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-state",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mutation := workflow.ClaimMutation{
		WorkItemID:    "work-item-state",
		ClaimID:       "claim-state",
		FencingToken:  3,
		OwnerID:       "collector-owner",
		ObservedAt:    now,
		LeaseDuration: time.Minute,
	}
	return scopeValue, generation, mutation
}

func stateWriterFacts(scopeValue scope.IngestionScope, generation scope.ScopeGeneration, now time.Time) <-chan facts.Envelope {
	return testfixtures.FactChannel([]facts.Envelope{{
		FactID:        "fact-state",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      "security_alert.repository_alert",
		StableFactKey: "alert:state",
		ObservedAt:    now,
		Payload:       map[string]any{"number": float64(1)},
	}})
}

func TestCommitClaimedScopeGenerationWithStateWritesInsideTheCommitTransaction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	scopeValue, generation, mutation := stateWriterFixture(now)

	var execsBefore int
	var committedAtWrite bool
	writer := func(ctx context.Context, exec collector.CommitExecutor) error {
		execsBefore = len(database.tx.execs)
		committedAtWrite = database.tx.committed
		_, err := exec.ExecContext(ctx, "INSERT INTO state_probe VALUES ($1)", "row")
		return err
	}

	err := store.CommitClaimedScopeGenerationWithState(
		context.Background(), mutation, scopeValue, generation,
		stateWriterFacts(scopeValue, generation, now), nil, writer,
	)
	if err != nil {
		t.Fatalf("CommitClaimedScopeGenerationWithState() error = %v, want nil", err)
	}
	if committedAtWrite {
		t.Error("state writer ran after commit; it must run inside the open transaction")
	}
	if execsBefore < 2 {
		t.Errorf("state writer ran after %d execs, want it after the claim fence and maintenance barrier", execsBefore)
	}
	var probe int
	for _, exec := range database.tx.execs {
		if strings.Contains(exec.query, "state_probe") {
			probe++
		}
	}
	if probe != 1 {
		t.Errorf("state writer statements on the commit transaction = %d, want 1", probe)
	}
	if !database.tx.committed {
		t.Error("transaction committed = false, want true")
	}
}

func TestCommitClaimedScopeGenerationWithStateRollsBackWhenTheWriterFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	scopeValue, generation, mutation := stateWriterFixture(now)
	cause := errors.New("state write failed")

	err := store.CommitClaimedScopeGenerationWithState(
		context.Background(), mutation, scopeValue, generation,
		stateWriterFacts(scopeValue, generation, now), nil,
		func(context.Context, collector.CommitExecutor) error { return cause },
	)
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want it to wrap the writer error", err)
	}
	if database.tx.committed {
		t.Error("transaction committed = true, want false when the state writer fails")
	}
	if !database.tx.rolledBack {
		t.Error("transaction rolledBack = false, want true")
	}
}

func TestCommitClaimedScopeGenerationWithStateSkipsTheWriterForUnchangedGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-active", "fingerprint-same", "", "active", false, true}},
		}},
	}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	scopeValue, generation, mutation := stateWriterFixture(now)
	generation.FreshnessHint = "fingerprint-same"

	called := false
	err := store.CommitClaimedScopeGenerationWithState(
		context.Background(), mutation, scopeValue, generation, nil, nil,
		func(context.Context, collector.CommitExecutor) error { called = true; return nil },
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if called {
		t.Error("state writer ran for a generation skipped as unchanged; nothing new commits then")
	}
	if database.beginCalls != 0 {
		t.Errorf("begin calls = %d, want 0 for an unchanged generation", database.beginCalls)
	}
}

func TestCommitClaimedScopeGenerationWithStateRequiresAWriter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(database)
	scopeValue, generation, mutation := stateWriterFixture(now)
	if err := store.CommitClaimedScopeGenerationWithState(
		context.Background(), mutation, scopeValue, generation,
		stateWriterFacts(scopeValue, generation, now), nil, nil,
	); err == nil {
		t.Fatal("error = nil, want an error for a nil state writer")
	}
	if database.beginCalls != 0 {
		t.Errorf("begin calls = %d, want 0 when the writer is missing", database.beginCalls)
	}
}
