// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// crossplaneRedriveHookOrderFake implements ExecQueryer + Beginner +
// Transaction (every surface ProjectorQueue.Ack needs) plus
// CrossplaneRedriveSweeper, recording every ExecContext call, the Commit
// call, and the Sweep call into one shared, ordered log. This proves
// ordering (P2-d) without a real database: Ack's five statement executions
// and its Commit must all precede the hook's Sweep call in the log.
type crossplaneRedriveHookOrderFake struct {
	log      *[]string
	sweepErr error
}

func (f crossplaneRedriveHookOrderFake) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	*f.log = append(*f.log, "exec")
	return driverResult{}, nil
}

func (f crossplaneRedriveHookOrderFake) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("query not expected in this test")
}

func (f crossplaneRedriveHookOrderFake) Begin(context.Context) (Transaction, error) {
	return f, nil
}

func (f crossplaneRedriveHookOrderFake) Commit() error {
	*f.log = append(*f.log, "commit")
	return nil
}

func (f crossplaneRedriveHookOrderFake) Rollback() error {
	*f.log = append(*f.log, "rollback")
	return nil
}

func (f crossplaneRedriveHookOrderFake) Sweep(context.Context, string, string) (CrossplaneRedriveSweepResult, error) {
	*f.log = append(*f.log, "hook-sweep")
	return CrossplaneRedriveSweepResult{}, f.sweepErr
}

// TestProjectorQueueAckInvokesCrossplaneRedriveHookAfterCommit proves the
// design's required ordering (issue #5476 P2-d): runCrossplaneRedriveHook
// fires strictly AFTER Ack's own transaction commits, never inside it, and a
// Sweep error is swallowed (logged, not returned) so it can never fail Ack --
// the generation is already correctly activated by the time the hook runs.
func TestProjectorQueueAckInvokesCrossplaneRedriveHookAfterCommit(t *testing.T) {
	var log []string
	fake := crossplaneRedriveHookOrderFake{log: &log, sweepErr: errors.New("injected sweep failure")}

	queue := ProjectorQueue{
		db:                fake,
		LeaseOwner:        "test-owner",
		LeaseDuration:     time.Minute,
		CrossplaneRedrive: fake,
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-hook-order"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-hook-order-001"},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("expected Ack to swallow the hook's Sweep error, got: %v", err)
	}

	if len(log) == 0 || log[len(log)-1] != "hook-sweep" {
		t.Fatalf("expected the hook's Sweep call to be the LAST event in the log, got %v", log)
	}
	commitIndex := -1
	sweepIndex := -1
	for i, event := range log {
		if event == "commit" {
			commitIndex = i
		}
		if event == "hook-sweep" {
			sweepIndex = i
		}
	}
	if commitIndex == -1 {
		t.Fatalf("expected a commit event in the log, got %v", log)
	}
	if sweepIndex == -1 {
		t.Fatalf("expected a hook-sweep event in the log, got %v", log)
	}
	if sweepIndex < commitIndex {
		t.Fatalf("expected hook-sweep (index %d) to occur AFTER commit (index %d), got %v", sweepIndex, commitIndex, log)
	}
}

// TestProjectorQueueAckSkipsHookWhenNilCrossplaneRedrive proves the hook is a
// pure no-op (never panics, never adds latency) when CrossplaneRedrive is
// unwired -- the default for every existing caller that predates this
// feature.
func TestProjectorQueueAckSkipsHookWhenNilCrossplaneRedrive(t *testing.T) {
	var log []string
	fake := crossplaneRedriveHookOrderFake{log: &log}

	queue := ProjectorQueue{
		db:            fake,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		// CrossplaneRedrive intentionally left nil.
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-hook-nil"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-hook-nil-001"},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	for _, event := range log {
		if event == "hook-sweep" {
			t.Fatalf("expected no hook-sweep event when CrossplaneRedrive is nil, got %v", log)
		}
	}
}
