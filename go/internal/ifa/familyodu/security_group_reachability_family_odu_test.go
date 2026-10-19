// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestSecurityGroupReachabilityFamilyOduStableKeysAreUnique pins that every
// fact in the security-group reachability Odù carries a distinct
// stable_fact_key (#6228 P1 lesson from iam_can_perform, repeated for
// iam_escalation, ec2_uses_profile, and s3_logs_to).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// fixture hangs nine rules off two groups, so the derivation carries the
// full rule identity (direction, protocol, port range, source) rather than
// the group alone. Key narrowness is invisible to every other check in this
// family (the offline guard runs over the compiled facts, never through
// ingest dedup), so it is pinned here rather than assumed from the
// derivation shape.
func TestSecurityGroupReachabilityFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := SecurityGroupReachabilityFamilyOdu().Odu
	if len(odu.Facts) != 12 {
		t.Fatalf("security-group reachability Odù carries %d facts, want 12 (3 anchors + 9 rules)", len(odu.Facts))
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
