// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestEC2UsesProfileFamilyOduStableKeysAreUnique pins that every fact in the
// ec2_uses_profile Odù carries a distinct stable_fact_key (#6228 P1 lesson
// from iam_can_perform, repeated for iam_escalation).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// fixture varies instance identity per posture (instance_id, with the ARN
// fallback where the collector emits no instance_id), so the derivation
// carries every collector identity input the fixture varies. Key narrowness is
// invisible to every other check in this family (the offline guard runs over
// the compiled facts, never through ingest dedup), so it is pinned here
// rather than assumed from the derivation shape.
func TestEC2UsesProfileFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := EC2UsesProfileFamilyOdu().Odu
	if len(odu.Facts) != 9 {
		t.Fatalf("ec2_uses_profile Odù carries %d facts, want 9 (3 profiles + 6 postures)", len(odu.Facts))
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
