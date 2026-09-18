// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

var errPreMaintenanceBoom = errors.New("boom")

// Pre-maintenance drains (#6184) wait for quiescence, not convergence: the
// fail-closed readiness gates keep deployment_mapping, workload
// materialization, and deployable-unit correlation retrying until the
// bootstrap-index maintenance pass publishes backward evidence, so a strict
// residual bound can never pass before maintenance runs. The
// -drain-allow-readiness-deferred mode passes when no LIVE work remains --
// every residual row waits on a readiness precondition -- and the
// post-maintenance strict drain re-checks everything.

func deferredOnlyQuerier() *fakeDrainQuerier {
	return &fakeDrainQuerier{
		seq: []DrainCounts{{FactWorkItemsResidual: 2}},
		breakdown: []residualRow{
			{Domain: "deployment_mapping", Status: "retrying", FailureClass: "cross_repo_backward_evidence_not_ready", Count: 1},
			{Domain: "workload_materialization", Status: "retrying", FailureClass: "workload_materialization_resolution_not_ready", Count: 1},
		},
	}
}

func TestPollPreMaintenancePassesOnReadinessDeferredOnly(t *testing.T) {
	counts, ok, err := pollUntilDrained(context.Background(), deferredOnlyQuerier(),
		strictDrainAssertions(), 0, time.Second, time.Millisecond, nil, 0, true)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatalf("expected pre-maintenance quiescence pass, got counts %+v", counts)
	}
}

func TestPollPreMaintenanceTreatsDeployableCanonicalNodesAsDeferred(t *testing.T) {
	counts := DrainCounts{FactWorkItemsResidual: 1}
	row := residualRow{
		Domain:       "deployable_unit_correlation",
		Status:       "retrying",
		FailureClass: reducer.DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass,
		Count:        1,
	}
	msg, quiescent := preMaintenanceQuiescence(counts, []residualRow{row})
	if !quiescent || !strings.Contains(msg, "live=0 readiness-deferred=1") {
		t.Fatalf("canonical-node readiness row must be deferred: quiescent=%t message=%q", quiescent, msg)
	}
	q := &fakeDrainQuerier{seq: []DrainCounts{counts}, breakdown: []residualRow{row}}
	_, ok, err := pollUntilDrained(context.Background(), q,
		strictDrainAssertions(), 0, time.Second, time.Millisecond, nil, 0, true)
	if err != nil || !ok {
		t.Fatalf("pre-maintenance poll must pass deferred row: ok=%t err=%v", ok, err)
	}

	row.Status = "claimed" // Claiming retains old failure metadata but is live work.
	if _, quiescent := preMaintenanceQuiescence(counts, []residualRow{row}); quiescent {
		t.Fatal("claimed row retaining readiness metadata must remain live")
	}
}

func TestPollPreMaintenanceStrictControlStillTimesOut(t *testing.T) {
	// Same deferred-only queue without the flag must keep polling to the
	// timeout: the flag is what reinterprets the residual, not the data.
	_, ok, err := pollUntilDrained(context.Background(), deferredOnlyQuerier(),
		strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond, nil, 0, false)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatal("strict drain must not converge on readiness-deferred residual")
	}
}

func TestPollPreMaintenanceWaitsForLiveWork(t *testing.T) {
	q := &fakeDrainQuerier{
		seq: []DrainCounts{{FactWorkItemsResidual: 2}},
		breakdown: []residualRow{
			{Domain: "deployment_mapping", Status: "retrying", FailureClass: "cross_repo_backward_evidence_not_ready", Count: 1},
			// No failure class: genuinely retrying live work, not a deferral.
			{Domain: "workload_materialization", Status: "retrying", FailureClass: "", Count: 1},
		},
	}
	_, ok, err := pollUntilDrained(context.Background(), q,
		strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond, nil, 0, true)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatal("pre-maintenance drain must not pass while live work remains")
	}
}

func TestPollPreMaintenanceRejectsTerminalRows(t *testing.T) {
	for _, row := range []residualRow{
		{Domain: "workload_materialization", Status: "dead_letter", FailureClass: "", Count: 1},
		{Domain: "workload_materialization", Status: "failed", FailureClass: "", Count: 1},
	} {
		q := &fakeDrainQuerier{
			seq:       []DrainCounts{{FactWorkItemsResidual: 1}},
			breakdown: []residualRow{row},
		}
		_, ok, err := pollUntilDrained(context.Background(), q,
			strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond, nil, 0, true)
		if err != nil {
			t.Fatalf("status %s: err = %v", row.Status, err)
		}
		if ok {
			t.Fatalf("pre-maintenance drain must not pass with %s rows", row.Status)
		}
	}
}

func TestPollPreMaintenanceBreakdownErrorKeepsPolling(t *testing.T) {
	q := &fakeDrainQuerier{
		seq:          []DrainCounts{{FactWorkItemsResidual: 2}},
		breakdownErr: errPreMaintenanceBoom,
	}
	_, ok, err := pollUntilDrained(context.Background(), q,
		strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond, nil, 0, true)
	if err != nil {
		t.Fatalf("breakdown read failure must degrade the verdict, not error: %v", err)
	}
	if ok {
		t.Fatal("pre-maintenance drain must not pass when the breakdown is unreadable")
	}
}

func TestPreMaintenanceQuiescenceMessageNamesDeferredState(t *testing.T) {
	// The six nonterminal shared intents are all repo_dependency rows: that
	// lane is fenced behind the maintenance pass, so they are the one shared
	// residual the pre-maintenance verdict tolerates.
	counts := DrainCounts{FactWorkItemsResidual: 2, SharedIntentsRequiredNonterminal: 6, RepoDependencyNonterminal: 6}
	rows := []residualRow{
		{Domain: "deployment_mapping", Status: "retrying", FailureClass: "cross_repo_backward_evidence_not_ready", Count: 1},
		{Domain: "workload_materialization", Status: "retrying", FailureClass: "workload_materialization_resolution_not_ready", Count: 1},
	}
	msg, quiescent := preMaintenanceQuiescence(counts, rows)
	if !quiescent {
		t.Fatal("deferred-only rows must report quiescent")
	}
	for _, want := range []string{"pre-maintenance", "readiness-deferred=2", "shared-required-nonterminal=6", "repo_dependency-deferred=6"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q must contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "more drain time would not have helped") {
		t.Errorf("pre-maintenance message must not claim time cannot help (maintenance is coming): %q", msg)
	}
}

func TestPreMaintenanceQuiescenceRejects(t *testing.T) {
	cases := map[string]struct {
		counts DrainCounts
		rows   []residualRow
	}{
		"live row": {
			counts: DrainCounts{FactWorkItemsResidual: 1},
			rows:   []residualRow{{Domain: "d", Status: "retrying", Count: 1}},
		},
		"claimed row retaining readiness failure": {
			counts: DrainCounts{FactWorkItemsResidual: 1},
			rows: []residualRow{{
				Domain: "workload_materialization", Status: "claimed",
				FailureClass: "workload_materialization_resolution_not_ready", Count: 1,
			}},
		},
		"running row retaining readiness failure": {
			counts: DrainCounts{FactWorkItemsResidual: 1},
			rows: []residualRow{{
				Domain: "deployment_mapping", Status: "running",
				FailureClass: "cross_repo_backward_evidence_not_ready", Count: 1,
			}},
		},
		"dead letter": {
			counts: DrainCounts{FactWorkItemsResidual: 1, FactWorkItemsDeadLetter: 1},
			rows:   []residualRow{{Domain: "d", Status: "dead_letter", Count: 1}},
		},
		"failed row": {
			counts: DrainCounts{FactWorkItemsResidual: 1},
			rows:   []residualRow{{Domain: "d", Status: "failed", Count: 1}},
		},
		"completion event pending": {
			counts: DrainCounts{CrossScopeCompletionEventsNonterminal: 1},
			rows:   []residualRow{{Domain: "d", Status: "retrying", FailureClass: "cross_repo_backward_evidence_not_ready", Count: 1}},
		},
		"shared intent outside repo_dependency": {
			counts: DrainCounts{FactWorkItemsResidual: 1, SharedIntentsRequiredNonterminal: 1},
			rows:   []residualRow{{Domain: "d", Status: "retrying", FailureClass: "cross_repo_backward_evidence_not_ready", Count: 1}},
		},
		"repo_dependency subset smaller than the required count": {
			counts: DrainCounts{SharedIntentsRequiredNonterminal: 3, RepoDependencyNonterminal: 2},
		},
	}
	for name, tc := range cases {
		if _, quiescent := preMaintenanceQuiescence(tc.counts, tc.rows); quiescent {
			t.Errorf("%s: must not report quiescent", name)
		}
	}
}

// #6747 shape B: Ifa fault-injection runs 35204957758 and 35222680019 passed
// the pre-maintenance drain with shared-required-nonterminal=14 and =4 while
// the rationale_edges lane was still consuming its intents, then failed the
// rationale exact-set assertion that follows the drain on the next line. A
// shared intent outside the maintenance-fenced repo_dependency lane is live
// work the lane will finish in seconds, not a readiness deferral, so the
// pre-maintenance verdict must keep polling for it exactly as the strict
// drain does.
func TestPollPreMaintenanceWaitsForSharedIntents(t *testing.T) {
	counts := DrainCounts{FactWorkItemsResidual: 9, SharedIntentsNonterminal: 14, SharedIntentsRequiredNonterminal: 14}
	rows := deferredOnlyQuerier().breakdown
	msg, quiescent := preMaintenanceQuiescence(counts, rows)
	if quiescent {
		t.Fatalf("nonterminal shared intents outside repo_dependency are live work: quiescent=%t message=%q", quiescent, msg)
	}
	q := &fakeDrainQuerier{seq: []DrainCounts{counts}, breakdown: rows}
	_, ok, err := pollUntilDrained(context.Background(), q,
		strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond, nil, 0, true)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatal("pre-maintenance drain must not pass while a shared lane still holds unconsumed intents")
	}
}

// The tolerance the mode was written for stays: repo_dependency intents are
// emitted by deployment_mapping and wait on the maintenance pass through the
// RUNS_ON workload readiness fence, so a residual made only of them is
// quiescent, and the poll converges once the other lanes drain.
func TestPollPreMaintenanceToleratesRepoDependencyIntents(t *testing.T) {
	rows := deferredOnlyQuerier().breakdown
	q := &fakeDrainQuerier{
		seq: []DrainCounts{
			{FactWorkItemsResidual: 9, SharedIntentsRequiredNonterminal: 5, RepoDependencyNonterminal: 2},
			{FactWorkItemsResidual: 9, SharedIntentsRequiredNonterminal: 2, RepoDependencyNonterminal: 2},
		},
		breakdown: rows,
	}
	counts, ok, err := pollUntilDrained(context.Background(), q,
		strictDrainAssertions(), 0, time.Second, time.Millisecond, nil, 0, true)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatalf("pre-maintenance drain must pass once only repo_dependency intents remain, got %+v", counts)
	}
	if counts.SharedIntentsRequiredNonterminal != 2 || q.i != 2 {
		t.Fatalf("poll must have waited for the second reading: counts=%+v polls=%d", counts, q.i)
	}
}
