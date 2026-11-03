// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestRangeEventsVulnerableFailsClosedOnAPairwiseUncomparableEvent pins that
// two events which each compare with the observed version but not with each
// other make the range malformed, rather than letting listed order pick a
// winner. The built-in comparators parse each operand alone, so only a fake
// pairwise comparator reaches this path.
func TestRangeEventsVulnerableFailsClosedOnAPairwiseUncomparableEvent(t *testing.T) {
	t.Parallel()
	compare := func(left, right string) (int, bool) {
		if (left == "a" && right == "b") || (left == "b" && right == "a") {
			return 0, false
		}
		rank := map[string]int{"a": 1, "b": 2, "z": 9}
		return rank[left] - rank[right], true
	}
	events := []supplychainmodel.AffectedRangeEvent{{Introduced: "a"}, {Fixed: "b"}}
	if got, valid := rangeEventsVulnerable(events, "z", compare, nil); valid || got {
		t.Fatalf("contains=%v valid=%v, want false false", got, valid)
	}
}

// TestRangeEventsVulnerableGoFloorSurvivesAPseudoVersion pins that the Go
// module floor ("0") applies to every observed version, including a
// pseudo-version below v0.0.0, which a plain compare would place under the floor.
func TestRangeEventsVulnerableGoFloorSurvivesAPseudoVersion(t *testing.T) {
	t.Parallel()
	const pseudo = "v0.0.0-20200101000000-abcdefabcdef"
	events := []supplychainmodel.AffectedRangeEvent{{Fixed: "1.5.0"}, {Introduced: "0"}}
	if got, valid := rangeEventsVulnerable(events, pseudo, compareOSVSemver, goModuleVersionFloor); !valid || !got {
		t.Fatalf("with the floor: contains=%v valid=%v, want true true", got, valid)
	}
	if got, valid := rangeEventsVulnerable(events, pseudo, compareOSVSemver, nil); !valid || got {
		t.Fatalf("without the floor: contains=%v valid=%v, want false true (the floor is what lifts it)", got, valid)
	}
}
