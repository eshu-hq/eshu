// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"sync"
	"testing"
)

// resetFactWriteStopWatchForTest drops the process-global write-stop watch so
// the next factWriteStopWatchFor binds the calling test's own database.
// factWriteStopWatchFor pins its database with a sync.Once, so without this a
// wiring test that executes a supply_chain_impact intent reads pg_trigger
// through whichever fake another test bound first and fails with
// "unexpected query". Callers must not run parallel with other tests that
// build the reducer service: the binding is process-global, so two builders
// racing would still hand one test the other's database.
func resetFactWriteStopWatchForTest() {
	factWriteStopWatch = nil
	factWriteStopOnce = sync.Once{}
}

// TestStateChangedLogsOnlyTheEdges pins the poller's log edges: the first reading and each change log,
// an unchanged reading does not.
func TestStateChangedLogsOnlyTheEdges(t *testing.T) {
	t.Parallel()

	last := -1
	for i, step := range []struct{ armed, want bool }{
		{true, true}, {true, false}, {false, true}, {false, false}, {true, true},
	} {
		if got := stateChanged(&last, step.armed); got != step.want {
			t.Fatalf("step %d: stateChanged(armed=%v) = %v, want %v", i, step.armed, got, step.want)
		}
	}
}
