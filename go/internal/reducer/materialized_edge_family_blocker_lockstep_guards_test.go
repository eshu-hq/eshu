// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// TestMaterializedEdgeFamilyBlockerLockstepCoversAllFamilies is the totality
// assertion: every family MaterializedEdgeFamilies() returns must be either
// covered by materializedEdgeFamilyBlockerExpectations or excluded with a
// named reason in materializedEdgeFamilyBlockerLockstepExclusions, never
// both and never neither. Without this, a 15th family added later would
// silently fall through this test looking green while asserting nothing
// about it; with it, an unclassified family fails loudly until someone
// deliberately covers or excludes it.
func TestMaterializedEdgeFamilyBlockerLockstepCoversAllFamilies(t *testing.T) {
	t.Parallel()

	all := make(map[string]bool, len(MaterializedEdgeFamilies()))
	for _, family := range MaterializedEdgeFamilies() {
		all[family] = true
	}

	classified := make(map[string]bool, len(all))
	for family := range materializedEdgeFamilyBlockerExpectations {
		if !all[family] {
			t.Errorf("materializedEdgeFamilyBlockerExpectations names family %q, which MaterializedEdgeFamilies() does not return -- stale entry", family)
			continue
		}
		classified[family] = true
	}
	for family, reason := range materializedEdgeFamilyBlockerLockstepExclusions {
		if !all[family] {
			t.Errorf("materializedEdgeFamilyBlockerLockstepExclusions names family %q, which MaterializedEdgeFamilies() does not return -- stale entry", family)
			continue
		}
		if classified[family] {
			t.Errorf("family %q is both covered by materializedEdgeFamilyBlockerExpectations and excluded (%q) -- pick one", family, reason)
			continue
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("family %q is excluded with a blank reason -- every exclusion needs a named reason", family)
		}
		classified[family] = true
	}

	for family := range all {
		if !classified[family] {
			t.Errorf("family %q from MaterializedEdgeFamilies() is neither covered by materializedEdgeFamilyBlockerExpectations nor excluded with a reason in materializedEdgeFamilyBlockerLockstepExclusions -- a new or renamed materialized-edge family must be classified deliberately before this test can pass", family)
		}
	}
}

// TestMaterializedEdgeFamilyBlockerLockstepCatchesWrongTableDeclaration is
// the deliberate-break tooth: it proves checkFamilyBlockerLockstep itself
// fails, and names the right thing, on the exact bug class this test exists
// to catch -- independent of file I/O, so it stays fast and deterministic as
// a permanent regression check. It calls checkFamilyBlockerLockstep directly
// with blockerSharedIntentLock for codeowners_ownership_edges' real
// (reflected) handler -- a shared_projection_intents lock declared for an
// EdgeWriter-only handler -- and asserts it is rejected. That declaration was
// once committed for this family; #5992 removed it and #6160 replaced it with
// a fact_records table lock, so the input here is now constructed rather than
// quoted from a live file. Keeping it costs nothing and the bug class it
// guards is what any future family can still hit.
func TestMaterializedEdgeFamilyBlockerLockstepCatchesWrongTableDeclaration(t *testing.T) {
	t.Parallel()

	const family = DomainCodeownersOwnershipEdges
	routedDomain := materializedEdgeFamilyBlockerExpectations[family].routedDomain
	handler := materializedEdgeFamilyHandlersByDomain()[routedDomain]

	err := checkFamilyBlockerLockstep(family, routedDomain, blockerSharedIntentLock, handler)
	if err == nil {
		t.Fatalf("checkFamilyBlockerLockstep(%q, shared_intent_lock, %T) = nil error, want an error: this handler has no IntentWriter field, so declaring shared_intent_lock must be caught", family, handler)
	}
	if !strings.Contains(err.Error(), family) {
		t.Errorf("error %q does not name the family %q", err.Error(), family)
	}
	if !strings.Contains(err.Error(), "IntentWriter") {
		t.Errorf("error %q does not explain the missing IntentWriter field", err.Error())
	}
}
