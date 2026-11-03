// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"

// rangeEventsVulnerable reports whether observed falls inside one OSV range,
// reading its introduced, fixed, and last_affected events the way the OSV
// reference algorithm does: sorted by version. The schema does not require a
// feed to list them sorted, and neither the collector nor the affected-package
// merge sorts them (#9865).
//
// Sorting is not needed to evaluate that. An event changes the state only when
// observed reaches it (introduced and fixed: observed is at or above the
// version; last_affected: observed is above it), and each triggered event sets
// the state outright (introduced: affected; fixed and last_affected: not
// affected). So the state after the sorted walk is the state the greatest
// triggered event sets. One pass keeps that event, in O(events) with no sort and
// no copy, and agrees with a stable sort on ties: among triggered events at one
// version, the one listed last wins, which is the order the walk used before
// this change.
//
// floor, when non-nil, names an introduced version that always applies,
// whatever observed is (the Go module "0" floor). Limit events are not read
// here; callers gate on them first. The second value is false when any event
// version cannot be compared, with observed or with the event it replaces.
func rangeEventsVulnerable(
	events []supplychainmodel.AffectedRangeEvent,
	observed string,
	compare versionCompareFunc,
	floor func(string) bool,
) (bool, bool) {
	var (
		bestVersion string
		bestOpens   bool
		haveBest    bool
	)
	for _, event := range events {
		var (
			version   string
			opens     bool
			triggered bool
			valid     = true
		)
		switch {
		case event.Introduced != "":
			version, opens = event.Introduced, true
			if floor != nil && floor(version) {
				triggered = true
			} else {
				triggered, valid = versionAtLeast(observed, version, compare)
			}
		case event.Fixed != "":
			version = event.Fixed
			triggered, valid = versionAtLeast(observed, version, compare)
		case event.LastAffected != "":
			version = event.LastAffected
			triggered, valid = versionGreaterThan(observed, version, compare)
		default:
			continue
		}
		if !valid {
			return false, false
		}
		if !triggered {
			continue
		}
		if haveBest {
			cmp, ok := compare(version, bestVersion)
			if !ok {
				return false, false
			}
			if cmp < 0 {
				continue
			}
		}
		bestVersion, bestOpens, haveBest = version, opens, true
	}
	return haveBest && bestOpens, true
}
