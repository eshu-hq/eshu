// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"sort"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestReadinessDeferredFailureClassesAreNonCounting keeps the gate's hand-kept
// readinessDeferredFailureClasses map in lockstep with the reducer queue's
// non-counting retry set in one direction: every class the gate treats as a
// readiness wait must also be exempt from the reducer retry budget. A class
// renamed or dropped on the reducer side would otherwise leave a stale entry
// here that makes a counting failure look like harmless deferral.
//
// The reverse direction is TestEveryNonCountingFailureClassIsEnrolledOrExcluded
// below (#7284): every non-counting class is either listed in the map or named
// in readinessLiveByDesignFailureClasses with a reason. The map stays a literal
// to keep the gate binary free of a runtime reducer dependency; only these
// tests import the storage package.
func TestReadinessDeferredFailureClassesAreNonCounting(t *testing.T) {
	t.Parallel()

	var counting []string
	for class := range readinessDeferredFailureClasses {
		if !storagepostgres.IsNonCountingReducerRetryFailureClass(class) {
			counting = append(counting, class)
		}
	}
	sort.Strings(counting)
	if len(counting) > 0 {
		t.Fatalf("readinessDeferredFailureClasses lists classes the reducer queue counts toward the retry budget: %v; "+
			"rename or remove them in lockstep with nonCountingReducerRetryFailureClasses", counting)
	}
}

// readinessLiveByDesignFailureClasses are the non-counting reducer retry
// classes the gate deliberately does NOT treat as readiness-deferred. A
// retrying row in one of these classes counts as live work, so
// pre-maintenance quiescence keeps waiting for it. Each entry carries the
// reason; an entry without one fails
// TestEveryNonCountingFailureClassIsEnrolledOrExcluded.
var readinessLiveByDesignFailureClasses = map[string]string{
	// #6686 / #7284: the generation-freshness check runs in front of every
	// reducer handler, so this class can sit on the families pre-maintenance
	// cells assert absent.
	"generation_activation_not_ready": "attaches to any reducer domain and resolves without the maintenance pass; " +
		"tolerating it would let pre-maintenance quiescence pass before a gated family's intent has evaluated its gate",
}

// TestEveryNonCountingFailureClassIsEnrolledOrExcluded is the reverse of
// TestReadinessDeferredFailureClassesAreNonCounting (#7284): every class the
// reducer queue exempts from the retry budget must carry an explicit gate
// decision, either enrolled in readinessDeferredFailureClasses or excluded in
// readinessLiveByDesignFailureClasses with a reason. Without it a new
// readiness class lands in storage/postgres, the gate counts its retrying rows
// as live, and pre-maintenance quiescence can time out on work only the
// maintenance pass would unblock — the gap #7284 found for twenty classes.
func TestEveryNonCountingFailureClassIsEnrolledOrExcluded(t *testing.T) {
	t.Parallel()

	nonCounting := storagepostgres.NonCountingReducerRetryFailureClasses()
	if len(nonCounting) == 0 {
		t.Fatal("storagepostgres.NonCountingReducerRetryFailureClasses returned no classes; " +
			"the reverse-direction guard would pass vacuously")
	}

	var undecided, both []string
	for _, class := range nonCounting {
		_, excluded := readinessLiveByDesignFailureClasses[class]
		enrolled := readinessDeferredFailureClasses[class]
		switch {
		case enrolled && excluded:
			both = append(both, class)
		case !enrolled && !excluded:
			undecided = append(undecided, class)
		}
	}

	var blankReason, staleExclusion []string
	for class, reason := range readinessLiveByDesignFailureClasses {
		if strings.TrimSpace(reason) == "" {
			blankReason = append(blankReason, class)
		}
		if !storagepostgres.IsNonCountingReducerRetryFailureClass(class) {
			staleExclusion = append(staleExclusion, class)
		}
	}

	sort.Strings(undecided)
	sort.Strings(both)
	sort.Strings(blankReason)
	sort.Strings(staleExclusion)
	if len(undecided) > 0 {
		t.Errorf("non-counting reducer retry classes with no gate decision: %v; enroll each in "+
			"readinessDeferredFailureClasses or exclude it in readinessLiveByDesignFailureClasses with a reason",
			undecided)
	}
	if len(both) > 0 {
		t.Errorf("classes both enrolled and excluded: %v; pick one", both)
	}
	if len(blankReason) > 0 {
		t.Errorf("readinessLiveByDesignFailureClasses entries with a blank reason: %v", blankReason)
	}
	if len(staleExclusion) > 0 {
		t.Errorf("readinessLiveByDesignFailureClasses lists classes the reducer queue no longer exempts: %v; "+
			"remove them in lockstep with nonCountingReducerRetryFailureClasses", staleExclusion)
	}
}
