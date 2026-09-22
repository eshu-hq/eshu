// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerQueueFailDefersCloudAdmissionReadinessPastAttemptBudget pins the
// #6887 admission-drain fence as a non-counting readiness miss: the exact
// error the handler returns (ClassifyCloudRetractError over the storage
// sentinel) drives Fail past MaxAttempts and the row stays 'retrying' in the
// cloud_admission_not_ready class instead of dead-lettering.
func TestReducerQueueFailDefersCloudAdmissionReadinessPastAttemptBudget(t *testing.T) {
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
	intent := reducer.Intent{IntentID: "intent-aws-resource-1", AttemptCount: 42}
	cause := reducer.ClassifyCloudRetractError(fmt.Errorf("retract dead cloud resource nodes: %w: scope-b/gen-2=pending",
		reducercontract.ErrCloudAdmissionUndrained))

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
	if got, want := database.execs[0].args[1], reducer.CloudAdmissionNotReadyFailureClass; got != want {
		t.Fatalf("failure class = %v, want %v", got, want)
	}
}
