// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

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
