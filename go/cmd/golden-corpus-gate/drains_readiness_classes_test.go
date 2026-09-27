// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"sort"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// nonCountingClasses returns the reducer queue's non-counting retry set and
// fails the test when it is empty, so no guard below can pass vacuously.
func nonCountingClasses(t *testing.T) []string {
	t.Helper()
	classes := storagepostgres.NonCountingReducerRetryFailureClasses()
	if len(classes) == 0 {
		t.Fatal("storagepostgres.NonCountingReducerRetryFailureClasses returned no classes; " +
			"every guard over the set would pass vacuously")
	}
	return classes
}

// TestEveryNonCountingFailureClassIsLabeledReadinessDeferred runs the
// production classifier over every class the reducer queue exempts from its
// retry budget (#7308): a retrying row in any of them is readiness-deferred,
// never live. A counting class and a row with no class stay live.
func TestEveryNonCountingFailureClassIsLabeledReadinessDeferred(t *testing.T) {
	t.Parallel()

	var mislabeled []string
	for _, class := range nonCountingClasses(t) {
		got := classifyResidualRows([]residualRow{{Domain: "d", Status: "retrying", FailureClass: class, Count: 1}})
		if got.readinessDeferred != 1 || got.live != 0 {
			mislabeled = append(mislabeled, class)
		}
	}
	sort.Strings(mislabeled)
	if len(mislabeled) > 0 {
		t.Errorf("non-counting classes not labeled readiness-deferred: %v", mislabeled)
	}

	for _, class := range []string{"", "graph_write_timeout", "projection_bug"} {
		got := classifyResidualRows([]residualRow{{Domain: "d", Status: "retrying", FailureClass: class, Count: 1}})
		if got.live != 1 || got.readinessDeferred != 0 || got.preMaintenanceBlocking != 0 {
			t.Errorf("counting class %q = %+v, want live 1", class, got)
		}
	}
}

// TestPreMaintenanceToleratedClassesAreNonCounting keeps the tolerated set a
// subset of the label set: a class renamed or dropped on the reducer side
// must not leave a stale entry that reads as a decision.
func TestPreMaintenanceToleratedClassesAreNonCounting(t *testing.T) {
	t.Parallel()

	if len(preMaintenanceToleratedFailureClasses) == 0 {
		t.Fatal("preMaintenanceToleratedFailureClasses is empty; the subset guard would pass vacuously")
	}
	var counting, blankReason []string
	for class, reason := range preMaintenanceToleratedFailureClasses {
		if !storagepostgres.IsNonCountingReducerRetryFailureClass(class) {
			counting = append(counting, class)
		}
		if strings.TrimSpace(reason) == "" {
			blankReason = append(blankReason, class)
		}
	}
	sort.Strings(counting)
	sort.Strings(blankReason)
	if len(counting) > 0 {
		t.Errorf("preMaintenanceToleratedFailureClasses lists classes the reducer queue counts toward the retry budget: %v; "+
			"rename or remove them in lockstep with nonCountingReducerRetryFailureClasses", counting)
	}
	if len(blankReason) > 0 {
		t.Errorf("preMaintenanceToleratedFailureClasses entries with a blank reason: %v", blankReason)
	}
}

// preMaintenanceBlockingFailureClasses are the non-counting classes that
// pre-maintenance quiescence deliberately does not tolerate. A retrying row in
// one of them is labeled readiness-deferred and still holds quiescence open.
// Each entry carries the reason.
var preMaintenanceBlockingFailureClasses = map[string]string{
	// #6686 / #7284: the generation-freshness check runs in front of every
	// reducer handler, so this class can sit on the families pre-maintenance
	// cells assert absent.
	"generation_activation_not_ready": "attaches to any reducer domain and resolves without the maintenance pass; " +
		"tolerating it would let pre-maintenance quiescence pass before a gated family's intent has evaluated its gate",
}

// TestEveryNonCountingFailureClassHasPreMaintenanceDecision forces a control
// decision for every class the reducer queue exempts from the retry budget
// (#7284, #7308): tolerated in preMaintenanceToleratedFailureClasses or
// blocking in preMaintenanceBlockingFailureClasses, never both and never
// neither. An undecided class already blocks quiescence at run time; this
// guard makes that a recorded decision instead of an accident.
func TestEveryNonCountingFailureClassHasPreMaintenanceDecision(t *testing.T) {
	t.Parallel()

	var undecided, both []string
	for _, class := range nonCountingClasses(t) {
		_, tolerated := preMaintenanceToleratedFailureClasses[class]
		_, blocking := preMaintenanceBlockingFailureClasses[class]
		switch {
		case tolerated && blocking:
			both = append(both, class)
		case !tolerated && !blocking:
			undecided = append(undecided, class)
		}
	}

	var blankReason, staleBlocking []string
	for class, reason := range preMaintenanceBlockingFailureClasses {
		if strings.TrimSpace(reason) == "" {
			blankReason = append(blankReason, class)
		}
		if !storagepostgres.IsNonCountingReducerRetryFailureClass(class) {
			staleBlocking = append(staleBlocking, class)
		}
	}

	sort.Strings(undecided)
	sort.Strings(both)
	sort.Strings(blankReason)
	sort.Strings(staleBlocking)
	if len(undecided) > 0 {
		t.Errorf("non-counting reducer retry classes with no pre-maintenance decision: %v; tolerate each in "+
			"preMaintenanceToleratedFailureClasses or block it in preMaintenanceBlockingFailureClasses with a reason",
			undecided)
	}
	if len(both) > 0 {
		t.Errorf("classes both tolerated and blocking: %v; pick one", both)
	}
	if len(blankReason) > 0 {
		t.Errorf("preMaintenanceBlockingFailureClasses entries with a blank reason: %v", blankReason)
	}
	if len(staleBlocking) > 0 {
		t.Errorf("preMaintenanceBlockingFailureClasses lists classes the reducer queue no longer exempts: %v; "+
			"remove them in lockstep with nonCountingReducerRetryFailureClasses", staleBlocking)
	}
}

// TestPreMaintenanceQuiescenceFollowsTheDecision runs the production predicate
// for every non-counting class: a lone retrying row is quiescent when the class
// is recorded as tolerated and not quiescent when it is recorded as blocking.
// It ties the test-only blocking map to what the gate does.
func TestPreMaintenanceQuiescenceFollowsTheDecision(t *testing.T) {
	t.Parallel()

	for _, class := range nonCountingClasses(t) {
		_, blocking := preMaintenanceBlockingFailureClasses[class]
		row := residualRow{Domain: "d", Status: "retrying", FailureClass: class, Count: 1}
		_, quiescent := preMaintenanceQuiescence(DrainCounts{FactWorkItemsResidual: 1}, []residualRow{row})
		if quiescent == blocking {
			t.Errorf("class %q: quiescent=%t, recorded blocking=%t", class, quiescent, blocking)
		}
	}
}
