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
// sorted(range.events). Limit-only events carry no range version, so the
// walk steps over them wherever they sit. A feed that already lists events in
// order takes a zero-allocation fast path returning the input slice, which
// the caller must only read. The second value is false when a version cannot
// be compared; the walk evaluates every version-bearing event with no early
// exit, so it would fail on that version in any order, and validating up
// front keeps the malformed contract while the sort order changes. The
// observed version is deliberately not validated here: with no
// version-bearing events the walk answers without comparing it, and that
// path is preserved.
func sortedRangeEventsForWalk(
	events []supplychainmodel.AffectedRangeEvent,
	compare versionCompareFunc,
) ([]supplychainmodel.AffectedRangeEvent, bool) {
	ascending, ok := rangeEventsListedAscending(events, compare)
	if !ok {
		return nil, false
	}
	if ascending {
		return events, true
	}
	type keyedEvent struct {
		event   supplychainmodel.AffectedRangeEvent
		version string
	}
	keyed := make([]keyedEvent, 0, len(events))
	for _, event := range events {
		version := rangeEventVersion(event)
		if version == "" {
			continue
		}
		keyed = append(keyed, keyedEvent{event: event, version: version})
	}
	slices.SortStableFunc(keyed, func(a, b keyedEvent) int {
		cmp, ok := compare(a.version, b.version)
		if !ok {
			return 0 // Unreachable: every version self-compared by the scan above.
		}
		return cmp
	})
	out := make([]supplychainmodel.AffectedRangeEvent, 0, len(keyed))
	for _, item := range keyed {
		out = append(out, item.event)
	}
	return out, true
}

// rangeEventVersion returns the version a range event orders by, or "" for a
// limit-only event, which carries no range version.
func rangeEventVersion(event supplychainmodel.AffectedRangeEvent) string {
	if event.Introduced != "" {
		return event.Introduced
	}
	if event.Fixed != "" {
		return event.Fixed
	}
	return event.LastAffected
}

// rangeEventsListedAscending reports whether the version-bearing events are
// already in non-decreasing version order, so the walk can read the feed
// order directly. The second value is false when a version cannot be
// compared. Unlike eventsAscending in ranges.go, ties do not fail here: a
// stable sort keeps tied events where the feed lists them, so an already
// scanned order is already the sorted order whatever the tie kinds are.
func rangeEventsListedAscending(
	events []supplychainmodel.AffectedRangeEvent,
	compare versionCompareFunc,
) (bool, bool) {
	previous := ""
	havePrevious := false
	for _, event := range events {
		version := rangeEventVersion(event)
		if version == "" {
			continue
		}
		if _, ok := compare(version, version); !ok {
			return false, false
		}
		if havePrevious {
			cmp, ok := compare(version, previous)
			if !ok {
				return false, false // Unreachable: both versions self-compared.
			}
			if cmp < 0 {
				return false, true
			}
		}
		previous, havePrevious = version, true
	}
	return true, true
}
