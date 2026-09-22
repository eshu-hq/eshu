// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// TestReducerQueueFailDefersValueFlowInputsReadinessPastAttemptBudget pins
// the #6923 value-flow refresh input fence as a non-counting readiness miss,
// mirroring #6892's
// TestReducerQueueFailDefersCloudAdmissionReadinessPastAttemptBudget: the
// exact error the refresh handler returns
// (crossscope.WrapValueFlowInputsUndrained) drives Fail past MaxAttempts and
// the singleton row stays 'retrying' in the value_flow_inputs_not_ready
// class instead of dead-lettering.
func TestReducerQueueFailDefersValueFlowInputsReadinessPastAttemptBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 22, 5, 0, 0, 0, time.UTC)
	database := &fakeExecQueryer{}
	queue := ReducerQueue{
		database:      database,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		RetryDelay:    2 * time.Minute,
		MaxAttempts:   3,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{IntentID: "intent-value-flow-refresh-global", AttemptCount: 42}
	cause := crossscope.WrapValueFlowInputsUndrained([]string{"scope-a/gen-1=code_function_summary:running"})

	if err := queue.Fail(context.Background(), intent, cause); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if got, want := len(database.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := database.execs[0].query
	for _, want := range []string{"UPDATE fact_work_items", "status = 'retrying'", "failure_class = $2"} {
		if !strings.Contains(query, want) {
			t.Fatalf("deferred retry query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "dead_letter") {
		t.Fatalf("readiness miss past the budget must not dead-letter:\n%s", query)
	}
	if got, want := database.execs[0].args[1], crossscope.ValueFlowInputsNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}
