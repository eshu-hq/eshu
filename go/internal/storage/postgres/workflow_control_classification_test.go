// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestWorkflowControlStoreFailClaimTerminalPersistsStructuredClassification
// proves the #7926 persistence round-trip: a terminal claim mutation carrying
// bounded operation, status code, failure class, and terminal flag must bind
// every field to the terminal-fail statement, so a later 400 stays
// distinguishable from any other terminal failure in the database.
func TestWorkflowControlStoreFailClaimTerminalPersistsStructuredClassification(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)

	err := store.FailClaimTerminal(context.Background(), workflow.ClaimMutation{
		WorkItemID:        "jira-item-1",
		ClaimID:           "jira-claim-1",
		FencingToken:      2,
		OwnerID:           "collector-jira-1",
		ObservedAt:        now,
		FailureClass:      "terminal",
		FailureMessage:    "jira provider failure: terminal",
		FailureOperation:  "collect_failure",
		FailureStatusCode: 400,
		FailureTerminal:   true,
	})
	if err != nil {
		t.Fatalf("FailClaimTerminal() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), 10; got != want {
		t.Fatalf("terminal mutation arg count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"failure_operation = NULLIF($8, '')",
		"failure_status_code = NULLIF($9, 0)",
		"failure_terminal = $10",
		"last_failure_operation = NULLIF($8, '')",
		"last_failure_status_code = NULLIF($9, 0)",
		"last_failure_terminal = $10",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("terminal claim query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "$11") {
		t.Fatalf("terminal claim query leaves an unused parameter hole:\n%s", query)
	}
	args := db.execs[0].args
	if got, want := args[7], "collect_failure"; got != want {
		t.Fatalf("failure_operation arg = %v, want %q", got, want)
	}
	if got, want := args[8], 400; got != want {
		t.Fatalf("failure_status_code arg = %v, want %d", got, want)
	}
	if got, want := args[9], true; got != want {
		t.Fatalf("failure_terminal arg = %v, want %v", got, want)
	}
}

// TestWorkflowControlStoreFailClaimRetryablePersistsStructuredClassification
// pins the retryable side: the same structured fields bind without a
// parameter hole and the terminal flag stays false.
func TestWorkflowControlStoreFailClaimRetryablePersistsStructuredClassification(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.October, 16, 12, 10, 0, 0, time.UTC)

	err := store.FailClaimRetryable(context.Background(), workflow.ClaimMutation{
		WorkItemID:        "jira-item-1",
		ClaimID:           "jira-claim-1",
		FencingToken:      2,
		OwnerID:           "collector-jira-1",
		ObservedAt:        now,
		VisibleAt:         now.Add(time.Minute),
		FailureClass:      "rate_limited",
		FailureMessage:    "jira provider failure: rate_limited",
		FailureOperation:  "collect_failure",
		FailureStatusCode: 429,
		FailureTerminal:   false,
	})
	if err != nil {
		t.Fatalf("FailClaimRetryable() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), 11; got != want {
		t.Fatalf("retryable mutation arg count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"failure_operation = NULLIF($9, '')",
		"failure_status_code = NULLIF($10, 0)",
		"failure_terminal = $11",
		"last_failure_operation = NULLIF($9, '')",
		"last_failure_status_code = NULLIF($10, 0)",
		"last_failure_terminal = $11",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("retryable claim query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "$12") {
		t.Fatalf("retryable claim query leaves an unused parameter hole:\n%s", query)
	}
}
