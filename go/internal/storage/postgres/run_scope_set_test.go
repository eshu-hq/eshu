// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const testRunScopeSetJSON = `{"collector_instance_id":"collector-vulnerability-intelligence",` +
	`"targets":[{"scope_id":"vuln-intel://osv/npm/_slot/3/0","source":"osv","ecosystem":"npm",` +
	`"queries":[{"ecosystem":"npm","package_name":"left-pad","version":"1.3.0"}]}]}`

// TestGetRunRequestedScopeSetReturnsRow proves the claim plane can read the
// run's requested_scope_set (eshu-hq/eshu#9432): the row the coordinator
// persisted at plan time is what claim-time transport resolves.
func TestGetRunRequestedScopeSetReturnsRow(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{testRunScopeSetJSON}}}},
	}
	store := NewWorkflowControlStore(db)

	got, err := store.GetRunRequestedScopeSet(context.Background(), "run-9")
	if err != nil {
		t.Fatalf("GetRunRequestedScopeSet() error = %v, want nil", err)
	}
	if got != testRunScopeSetJSON {
		t.Fatalf("GetRunRequestedScopeSet() = %q, want persisted JSON", got)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	if !strings.Contains(db.queries[0].query, "FROM workflow_runs") {
		t.Fatalf("query %q does not read workflow_runs", db.queries[0].query)
	}
	if len(db.queries[0].args) != 1 || db.queries[0].args[0] != "run-9" {
		t.Fatalf("query args = %v, want [run-9]", db.queries[0].args)
	}
}

// TestGetRunRequestedScopeSetMissingRun proves a claim for an unknown run
// fails instead of resolving an empty transport.
func TestGetRunRequestedScopeSetMissingRun(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	store := NewWorkflowControlStore(db)

	_, err := store.GetRunRequestedScopeSet(context.Background(), "run-missing")
	if err == nil {
		t.Fatal("GetRunRequestedScopeSet() accepted a missing run, want error")
	}
	if !errors.Is(err, workflow.ErrRunNotFound) {
		t.Fatalf("GetRunRequestedScopeSet() error = %v, want workflow.ErrRunNotFound", err)
	}
}

// TestGetRunRequestedScopeSetQueryErrorIsNotNotFound proves a transport
// read fault stays distinguishable from a missing run row: the claim plane
// must retry the former and fail the latter terminal (eshu-hq/eshu#9730).
func TestGetRunRequestedScopeSetQueryErrorIsNotNotFound(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("connection reset by peer")}},
	}
	store := NewWorkflowControlStore(db)

	_, err := store.GetRunRequestedScopeSet(context.Background(), "run-9")
	if err == nil {
		t.Fatal("GetRunRequestedScopeSet() accepted a failed read, want error")
	}
	if errors.Is(err, workflow.ErrRunNotFound) {
		t.Fatalf("GetRunRequestedScopeSet() error = %v matches ErrRunNotFound, want a plain transport error", err)
	}
}

// TestGetRunRequestedScopeSetRejectsBlankRunID proves the lookup never
// issues an unkeyed read.
func TestGetRunRequestedScopeSetRejectsBlankRunID(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)

	if _, err := store.GetRunRequestedScopeSet(context.Background(), "  "); err == nil {
		t.Fatal("GetRunRequestedScopeSet() accepted a blank run id, want error")
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0", len(db.queries))
	}
}
