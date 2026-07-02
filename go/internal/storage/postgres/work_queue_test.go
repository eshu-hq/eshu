// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// This file holds the core ProjectorQueue claim/enqueue/fail unit tests. The
// sibling ReducerQueue tests live in reducer_queue_basic_test.go — split out
// (#4450) to keep both files under the repo's 500-line cap after the
// exponential-backoff-with-jitter change added assertions and comments to
// several retry tests.

func TestProjectorQueueClaimReturnsScopeGenerationWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"scope-123",
					"git",
					"repository",
					"",
					"generation-active",
					true,
					"git",
					"repo-123",
					"generation-456",
					1,
					time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC),
					time.Date(2026, time.April, 12, 10, 5, 0, 0, time.UTC),
					"pending",
					"snapshot",
					"",
					[]byte(`{"repo_id":"repository:r_test"}`),
				}},
			},
		},
	}

	queue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	work, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if got, want := work.Scope.ScopeID, "scope-123"; got != want {
		t.Fatalf("Claim().Scope.ScopeID = %q, want %q", got, want)
	}
	if got, want := work.Generation.GenerationID, "generation-456"; got != want {
		t.Fatalf("Claim().Generation.GenerationID = %q, want %q", got, want)
	}
	if got, want := work.AttemptCount, 1; got != want {
		t.Fatalf("Claim().AttemptCount = %d, want %d", got, want)
	}
	if got, want := work.Scope.ActiveGenerationID, "generation-active"; got != want {
		t.Fatalf("Claim().Scope.ActiveGenerationID = %q, want %q", got, want)
	}
	if !work.Scope.PreviousGenerationExists {
		t.Fatal("Claim().Scope.PreviousGenerationExists = false, want true")
	}
	if !strings.Contains(db.queries[0].query, "stage = 'projector'") {
		t.Fatalf("claim query = %q, want projector stage filter", db.queries[0].query)
	}
}

func TestProjectorQueueClaimPopulatesScopeMetadataFromPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"scope-123",
					"git",
					"repository",
					"",
					"",
					false,
					"git",
					"repo-123",
					"generation-456",
					2,
					time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC),
					time.Date(2026, time.April, 12, 10, 5, 0, 0, time.UTC),
					"pending",
					"snapshot",
					"",
					[]byte(`{"repo_id":"repository:r_test","source_key":"repo-123"}`),
				}},
			},
		},
	}

	queue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	work, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Claim() ok = false, want true")
	}
	if got, want := work.Scope.Metadata["repo_id"], "repository:r_test"; got != want {
		t.Fatalf("Claim().Scope.Metadata[repo_id] = %q, want %q", got, want)
	}
}

func TestProjectorQueueEnqueueInsertsProjectorStageWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ProjectorQueue{
		db:  db,
		Now: func() time.Time { return now },
	}

	err := queue.Enqueue(
		context.Background(),
		"scope-123",
		"generation-456",
	)
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO fact_work_items") {
		t.Fatalf("enqueue query = %q, want fact_work_items insert", db.execs[0].query)
	}
	if got, want := db.execs[0].args[3], "source_local"; got != want {
		t.Fatalf("domain arg = %v, want %v", got, want)
	}
}

func TestProjectorQueueFailRetriesGraphWriteTimeoutWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID: "scope-123",
		},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
		AttemptCount: 1,
	}
	cause := fmt.Errorf("canonical projection: %w", sourcecypher.GraphWriteTimeoutError{
		Operation:   "neo4j execute timed out",
		Timeout:     30 * time.Second,
		TimeoutHint: "ESHU_CANONICAL_WRITE_TIMEOUT",
		Summary:     "phase=entities label=Function rows=15",
		Cause:       context.DeadlineExceeded,
	})

	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'retrying'") {
		t.Fatalf("timeout should retry within attempt budget, query:\n%s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[1], "projection_retryable"; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], "phase=entities label=Function rows=15"; got != want {
		t.Fatalf("failure details = %v, want %v", got, want)
	}
	// Exponential backoff (#4450): AttemptCount=1 doubles the 2m base delay to
	// 4m (baseDelay*(1<<attempt)); JitterFraction is unset (0) so this stays
	// deterministic with no added jitter term.
	if got, want := db.execs[0].args[4], now.Add(4*time.Minute); got != want {
		t.Fatalf("next attempt = %v, want %v", got, want)
	}
}
