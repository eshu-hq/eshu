// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestWorkloadCloudRelationshipFamilyOduStableKeysAreUnique pins that every
// fact in the workload_cloud_relationship Odù carries a distinct
// stable_fact_key (#6228 P1 lesson from iam_can_perform).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The key
// here derives from account:region:resource_type:resource_id, so five
// distinct resources cannot share one — but key narrowness is invisible to
// every other check in this family (the offline guard runs over the compiled
// facts, never through ingest dedup), so it is pinned here rather than
// assumed from the derivation shape.
func TestWorkloadCloudRelationshipFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := WorkloadCloudRelationshipFamilyOdu().Odu
	if len(odu.Facts) != 5 {
		t.Fatalf("workload_cloud_relationship Odù carries %d facts, want 5 (2 edge-producing anchors + 3 deliberate non-producers)", len(odu.Facts))
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
