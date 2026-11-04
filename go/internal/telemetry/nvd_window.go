// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

// Closed NVD rolling-window planner codes (eshu-hq/eshu#9877, item 2.2,
// arbiter decision 11). They ride span attributes and validator errors, never
// metric labels: labels stay source-only so a window value cannot explode
// cardinality.
const (
	// NVDWindowEndAfter marks an end past the allowable head: the clamp plus
	// a skew signal for a planner-generated window, and a hard reject only
	// for an operator-configured static window at target validation.
	NVDWindowEndAfter = "window_end_after"
	// NVDWindowExceedsMaxRange marks a window wider than the admission cap:
	// the planner chunks it, and a static config carrying one is rejected.
	NVDWindowExceedsMaxRange = "window_exceeds_max_range"
	// NVDWindowOlderThanMinimum marks a cursor more than the cap behind the
	// head. There is no minimum: the claim chunks forward sequentially, and
	// a reject there would strand catch-up permanently.
	NVDWindowOlderThanMinimum = "window_older_than_minimum"
	// NVDWindowOther counts a record with a reason outside this set.
	NVDWindowOther = "other"
)

// nvdWindowCodes is the closed, ordered reason set.
var nvdWindowCodes = [...]string{
	NVDWindowEndAfter,
	NVDWindowExceedsMaxRange,
	NVDWindowOlderThanMinimum,
	NVDWindowOther,
}

// NVDWindowCodes returns the closed, ordered set of NVD window codes.
func NVDWindowCodes() []string {
	return slices.Clone(nvdWindowCodes[:])
}

// NVDWindowCode maps code onto the closed set.
func NVDWindowCode(code string) string {
	if slices.Contains(nvdWindowCodes[:], code) {
		return code
	}
	return NVDWindowOther
}
