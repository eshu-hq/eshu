// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// sortedEventsContainDecision walks range events in version order, as OSV
// evaluates them (sorted(range.events)), instead of the order the feed lists
// them (#9865). Every ecosystem that reads range events shares this walk.
func sortedEventsContainDecision(
	events []supplychainmodel.AffectedRangeEvent,
	observed string,
	compare versionCompareFunc,
) (bool, bool) {
	sorted, ok := sortedRangeEventsForWalk(events, compare)
	if !ok {
		return false, false
	}
	vulnerable := false
	for _, event := range sorted {
		switch {
		case event.Introduced != "":
			if atLeast, valid := versionAtLeast(observed, event.Introduced, compare); !valid {
				return false, false
			} else if atLeast {
				vulnerable = true
			}
		case event.Fixed != "":
			if atLeast, valid := versionAtLeast(observed, event.Fixed, compare); !valid {
				return false, false
			} else if atLeast {
				vulnerable = false
			}
		case event.LastAffected != "":
			if after, valid := versionGreaterThan(observed, event.LastAffected, compare); !valid {
				return false, false
			} else if after {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

// sortedRangeEventsForWalk returns the version-bearing events ordered by
// version, as OSV evaluates them: a stable sort, so events tied at one
// version keep the order the feed lists them, exactly like the reference
// sorted(range.events). Limit-only events carry no range version and never
// move the walk, so they are dropped. The second value is false when a
// version cannot be compared; the walk evaluates every version-bearing event
// with no early exit, so it would fail on that version in any order, and
// validating up front keeps the malformed contract while the sort order
// changes. The observed version is deliberately not validated here: with no
// version-bearing events the walk answers without comparing it, and that
// path is preserved.
func sortedRangeEventsForWalk(
	events []supplychainmodel.AffectedRangeEvent,
	compare versionCompareFunc,
) ([]supplychainmodel.AffectedRangeEvent, bool) {
	type keyedEvent struct {
		event   supplychainmodel.AffectedRangeEvent
		version string
	}
	keyed := make([]keyedEvent, 0, len(events))
	for _, event := range events {
		version := event.Introduced
		if version == "" {
			version = event.Fixed
		}
		if version == "" {
			version = event.LastAffected
		}
		if version == "" {
			continue
		}
		if _, ok := compare(version, version); !ok {
			return nil, false
		}
		keyed = append(keyed, keyedEvent{event: event, version: version})
	}
	slices.SortStableFunc(keyed, func(a, b keyedEvent) int {
		cmp, ok := compare(a.version, b.version)
		if !ok {
			return 0 // Unreachable: every version self-compared above.
		}
		return cmp
	})
	out := make([]supplychainmodel.AffectedRangeEvent, 0, len(keyed))
	for _, item := range keyed {
		out = append(out, item.event)
	}
	return out, true
}
