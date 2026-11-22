// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

// selectionBackpressureSupportsMode is the starting point of the schedule-leg
// mode predicate: it accepts every mode, which is the behavior the leg shipped
// with. The regression tests pin what it must reject.
func selectionBackpressureSupportsMode(string) bool {
	return true
}
