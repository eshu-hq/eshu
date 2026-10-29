// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreWithheldPartialIsFencedAndBounded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		terminal   bool
		stateParam string
		wantArgs   int
	}{
		{name: "retryable", stateParam: "$12", wantArgs: 14},
		{name: "terminal after attempt budget", terminal: true, stateParam: "$11", wantArgs: 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mutation := workflow.ClaimMutation{
				WorkItemID: "item", ClaimID: "claim", FencingToken: 7, OwnerID: "worker",
				ObservedAt: now, VisibleAt: now.Add(time.Minute),
				FailureClass: "partial_snapshot_withheld", FailureTerminal: tc.terminal,
				ExtensionOutcome: &workflow.ExtensionOutcome{
					State: "partial_withheld", Partial: true, WarningCount: 3,
				},
			}
			for _, affected := range []int64{1, 0} {
				storeDB := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: affected}}}
				store := NewWorkflowControlStore(storeDB)
				var err error
				if tc.terminal {
					err = store.FailClaimTerminal(context.Background(), mutation)
				} else {
					err = store.FailClaimRetryable(context.Background(), mutation)
				}
				if affected == 0 {
					if !errors.Is(err, workflow.ErrClaimRejected) {
						t.Fatalf("stale claim error = %v, want ErrClaimRejected", err)
					}
				} else if err != nil {
					t.Fatalf("fenced partial mutation error = %v", err)
				}
				if len(storeDB.execs) != 1 {
					t.Fatalf("executions = %d, want 1", len(storeDB.execs))
				}
				exec := storeDB.execs[0]
				if len(exec.args) != tc.wantArgs || exec.args[tc.wantArgs-3] != "partial_withheld" ||
					exec.args[tc.wantArgs-2] != true || exec.args[tc.wantArgs-1] != 3 {
					t.Fatalf("bounded partial args = %#v", exec.args)
				}
				for _, fragment := range []string{
					"current_fencing_token", "current_owner_id", "current_claim_id",
					"extension_result_state = COALESCE(" + tc.stateParam,
				} {
					if !strings.Contains(exec.query, fragment) {
						t.Fatalf("claim mutation missing %q", fragment)
					}
				}
			}
		})
	}
}
