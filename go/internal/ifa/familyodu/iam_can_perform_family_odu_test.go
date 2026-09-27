// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestIAMCanPerformFamilyOduStableKeysAreUnique pins that every fact in the
// iam_can_perform Odù carries a distinct stable_fact_key (#6228 P1).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. A
// derivation keyed on principal:source:effect:actions alone collapses the
// four statements sharing deployer:inline:Allow:s3:getobject (KMS
// type-mismatch, NotAction, wildcard, ghost bucket) into one row: the live
// cell drives 17 facts instead of 20 and three negative controls never reach
// the extractor, while the offline guard — which runs over all 20 facts —
// stays green. Key narrowness is invisible to every other check in this
// family, so it is pinned here.
func TestIAMCanPerformFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := IAMCanPerformFamilyOdu().Odu
	if len(odu.Facts) != 20 {
		t.Fatalf("iam_can_perform Odù carries %d facts, want 20 (6 resources + 14 statements)", len(odu.Facts))
	}
	seen := make(map[string]string, len(odu.Facts))
	for _, env := range odu.Facts {
		if env.StableFactKey == "" {
			t.Fatalf("fact kind %q carries an empty stable_fact_key", env.FactKind)
		}
		if prev, dup := seen[env.StableFactKey]; dup {
			t.Fatalf("duplicate stable_fact_key %q (fact kinds %s and %s): live ingest would collapse these to one row", env.StableFactKey, prev, env.FactKind)
		}
		seen[env.StableFactKey] = env.FactKind
	}
}
