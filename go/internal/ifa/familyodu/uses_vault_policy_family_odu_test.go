// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestUsesVaultPolicyFamilyOduStableKeysAreUnique pins that every fact in
// the uses_vault_policy Odù carries a distinct stable_fact_key (#6228 P1
// lesson from iam_can_perform, repeated for iam_escalation,
// ec2_uses_profile, s3_logs_to, and authenticates_vault_role).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// fixture varies identity per fact (the join key or ARN each fact carries),
// so the derivation carries every collector identity input the fixture
// varies. Key narrowness is invisible to every other check in this family
// (the offline guard runs over the compiled facts, never through ingest
// dedup), so it is pinned here rather than assumed from the derivation
// shape.
func TestUsesVaultPolicyFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := UsesVaultPolicyFamilyOdu().Odu
	if len(odu.Facts) != 39 {
		t.Fatalf("uses_vault_policy Odù carries %d facts, want 39 (two exact lanes plus five non-producing controls plus four endpoint observations)", len(odu.Facts))
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
