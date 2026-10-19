// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestUsesServiceAccountFamilyOduStableKeysAreUnique pins that every fact in
// the uses_service_account Odù carries a distinct stable_fact_key (#6228 P1
// lesson from iam_can_perform, repeated for iam_escalation,
// ec2_uses_profile, s3_logs_to, and the security-group reachability pair).
//
// Read-model facts upsert ON CONFLICT (fact_id) like collector facts, so two
// chains sharing a key would collapse last-writer-wins at ingest and the
// offline guard — which runs over the compiled facts, never through ingest
// dedup — could not see the loss. The derivation keys on the service-account
// join key, which is distinct per fixture chain. Key narrowness is invisible
// to every other check in this family, so it is pinned here rather than
// assumed from the derivation shape.
func TestUsesServiceAccountFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := UsesServiceAccountFamilyOdu().Odu
	if len(odu.Facts) != 5 {
		t.Fatalf("uses_service_account Odù carries %d facts, want 5", len(odu.Facts))
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
