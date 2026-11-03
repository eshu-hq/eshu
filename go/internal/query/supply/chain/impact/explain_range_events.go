// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// explainEventsVulnerable reports whether observed falls inside one range
// under the OSV algorithm, which evaluates the events sorted by version. The
// schema does not require a feed to list them sorted (#9865). Each event
// changes the state only when observed reaches it, and sets the state
// outright, so the state after the sorted walk is the one the greatest
// triggered event sets; ties go to the event listed last. The reducer's
// rangeEventsVulnerable does the same; the two stay separate because the
// query layer must not import the reducer. The second value is false when any
// compared version is not a parseable semver-family version.
func explainEventsVulnerable(events []explainAffectedRangeEvent, observed string) (bool, bool) {
	var (
		bestVersion string
		bestOpens   bool
		haveBest    bool
	)
	for _, event := range events {
		var (
			version string
			opens   bool
			strict  bool
		)
		switch {
		case strings.TrimSpace(event.introduced) != "":
			version, opens = event.introduced, true
		case strings.TrimSpace(event.fixed) != "":
			version = event.fixed
		case strings.TrimSpace(event.lastAffected) != "":
			version, strict = event.lastAffected, true
		default:
			continue
		}
		cmp, ok := compareExplainSemverVersions(observed, version)
		if !ok {
			return false, false
		}
		if cmp < 0 || (strict && cmp == 0) {
			continue
		}
		if haveBest {
			order, ok := compareExplainSemverVersions(version, bestVersion)
			if !ok {
				return false, false
			}
			if order < 0 {
				continue
			}
		}
		bestVersion, bestOpens, haveBest = version, opens, true
	}
	return haveBest && bestOpens, true
}

// lowestFixedAbove returns the lowest Fixed boundary in events that is above
// observed, which ends the pair containing it. Empty when none is above it or
// when a boundary is not a parseable semver-family version.
func lowestFixedAbove(events []explainAffectedRangeEvent, observed string) string {
	lowest := ""
	for _, event := range events {
		fixed := strings.TrimSpace(event.fixed)
		if fixed == "" {
			continue
		}
		if cmp, ok := compareExplainSemverVersions(observed, fixed); !ok || cmp >= 0 {
			continue
		}
		if lowest == "" {
			lowest = fixed
			continue
		}
		if cmp, ok := compareExplainSemverVersions(fixed, lowest); ok && cmp < 0 {
			lowest = fixed
		}
	}
	return lowest
}
