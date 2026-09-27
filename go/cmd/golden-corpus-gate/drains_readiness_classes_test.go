// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"sort"
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
// The reverse does not hold today and is not asserted: the non-counting set
// also carries graph-node readiness classes (for example
// aws_relationship_nodes_not_ready) that this map does not list (#7258
// review). The map stays a literal to keep the gate binary free of a runtime
// reducer dependency; only this test imports the storage package.
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
