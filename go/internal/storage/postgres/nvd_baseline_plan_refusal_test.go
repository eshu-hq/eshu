// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestWorkflowControlStoreRefusesBaselinePlansItCannotTrust proves the store
// checks the coordinator's plan list before it opens a transaction: a plan for
// a scope the admission does not carry, one whose run or year disagrees with
// its scope id, a duplicate year, a plan against a non-vulnerability item, and
// plans with no work items at all. Each would otherwise write a pointer to a
// chunk that was never inserted or to the wrong year.
func TestWorkflowControlStoreRefusesBaselinePlansItCannotTrust(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	_, items, plans := baselineAdmissionFixture(2026, 2025)
	awsRun, awsItems := workflowPlanningLockFixture(t)
	foreign := baselineChunkItem(awsRun.RunID, "collector-aws", 2024, baselineTestStart, now)
	foreign.CollectorKind = scope.CollectorAWS

	vulnRun, _, _ := baselineAdmissionFixture()
	cases := map[string]struct {
		run   workflow.Run
		items []workflow.WorkItem
		plans []workflow.NVDBaselinePlan
		want  string
	}{
		"scope not in the admission": {
			run:   vulnRun,
			items: items[:1],
			plans: plans[1:],
			want:  "not in the admission",
		},
		"year disagrees with the scope": {
			run:   vulnRun,
			items: items,
			plans: []workflow.NVDBaselinePlan{{BaselineRunID: baselineTestRunID, Year: 2020, ScopeID: items[0].ScopeID}},
			want:  "disagrees with its scope id",
		},
		"run disagrees with the scope": {
			run:   vulnRun,
			items: items,
			plans: []workflow.NVDBaselinePlan{{BaselineRunID: "other-run", Year: 2026, ScopeID: items[0].ScopeID}},
			want:  "disagrees with its scope id",
		},
		"not a baseline scope": {
			run:   awsRun,
			items: awsItems,
			plans: []workflow.NVDBaselinePlan{{BaselineRunID: baselineTestRunID, Year: 2026, ScopeID: awsItems[0].ScopeID}},
			want:  "not an NVD baseline scope",
		},
		"duplicate year": {
			run:   vulnRun,
			items: items,
			plans: []workflow.NVDBaselinePlan{plans[0], {BaselineRunID: baselineTestRunID, Year: 2026, ScopeID: plans[0].ScopeID}},
			want:  "twice",
		},
		"item of another collector kind": {
			run:   awsRun,
			items: []workflow.WorkItem{foreign},
			plans: []workflow.NVDBaselinePlan{{BaselineRunID: baselineTestRunID, Year: 2024, ScopeID: foreign.ScopeID}},
			want:  "not a vulnerability_intelligence work item",
		},
		"plans without work items": {
			run:   vulnRun,
			items: nil,
			plans: plans,
			want:  "not in the admission",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recorder := &orderedStatementRecorder{}
			store := NewWorkflowControlStore(recorder)

			_, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), tc.run, tc.items, tc.plans, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
			if len(recorder.statements) != 0 || recorder.didCommit() {
				t.Fatalf("the store issued %d statements for a plan list it refused; it must fail before any statement", len(recorder.statements))
			}
		})
	}
}
