// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import (
	"strings"
	"testing"
)

// TestCVEProbeIsChunkedBelowThePlanFlip pins the #9994 bound. The bound must sit
// below the lowest list length whose custom plan walks the advisory index, which
// QA measured at 172 ids, because a chunked list's last statement can be any
// length from 1 up to the bound. A bound above 171 is unsafe even when the bound
// itself plans well: 240 does, and a 180-id tail does not.
func TestCVEProbeIsChunkedBelowThePlanFlip(t *testing.T) {
	t.Parallel()

	const lowestMeasuredWalk = 172
	if MaxCVEIDsPerStatement <= 0 || MaxCVEIDsPerStatement >= lowestMeasuredWalk {
		t.Fatalf("MaxCVEIDsPerStatement = %d, want a positive bound below %d", MaxCVEIDsPerStatement, lowestMeasuredWalk)
	}
	assertCVEProbesKeepTheBound(t, Branches())
}

// assertCVEProbesKeepTheBound finds the probes by anchor, not by name, so a new key
// probe on the CVE index is held to the same bound without editing this test.
func assertCVEProbesKeepTheBound(t *testing.T, branches []Branch) {
	t.Helper()
	probes := 0
	for _, branch := range branches {
		if branch.Anchor != IndexVulnerabilityCVEV2 || branch.Shape != ShapeKeyProbe {
			continue
		}
		probes++
		if branch.MaxListLength != MaxCVEIDsPerStatement {
			t.Errorf("%s MaxListLength = %d, want %d", branch.Name, branch.MaxListLength, MaxCVEIDsPerStatement)
		}
	}
	if probes == 0 {
		t.Fatalf("catalog has no key probe anchored on %s", IndexVulnerabilityCVEV2)
	}
}

// TestOnlyExactBranchesAreChunked: a loader may split a list across statements
// only where each row answers to one list entry. That needs a key probe with one
// bound list and no LIMIT. The suppression statement carries a LIMIT and must see
// the whole list at once, and the guard and full-filter statements bind several
// lists.
func TestOnlyExactBranchesAreChunked(t *testing.T) {
	t.Parallel()

	for _, branch := range append(Branches(), ProtectedStaticBranches()...) {
		if branch.MaxListLength == 0 {
			continue
		}
		if branch.Shape != ShapeKeyProbe {
			t.Errorf("%s: shape %s is chunked, want only %s", branch.Name, branch.Shape, ShapeKeyProbe)
		}
		if len(branch.Params) != 1 {
			t.Errorf("%s: chunked with %d bound lists, want 1", branch.Name, len(branch.Params))
		}
		if strings.Contains(branch.SQL, "\nLIMIT ") {
			t.Errorf("%s: chunked statement carries a LIMIT", branch.Name)
		}
	}
}

// TestProtectedCatalogKeepsTheCVEChunkBound: the protected variant only rewrites
// the candidate predicate, so the same long list must not reach it whole.
func TestProtectedCatalogKeepsTheCVEChunkBound(t *testing.T) {
	t.Parallel()

	assertCVEProbesKeepTheBound(t, ProtectedStaticBranches())
}
