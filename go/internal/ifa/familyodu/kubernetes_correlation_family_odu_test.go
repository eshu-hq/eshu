// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestKubernetesCorrelationFamilyOduStableKeysAreUnique pins that every
// fact in the kubernetes_correlation Odù carries a distinct stable_fact_key
// (#6228 P1 lesson from iam_can_perform, repeated for iam_escalation,
// ec2_uses_profile, and s3_logs_to).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// fixture varies workload object_id per pod template and repository+digest
// (or repository+tag) per OCI observation, so the derivation carries every
// collector identity input the fixture varies. Key narrowness is invisible
// to every other check in this family (the offline guard runs over the
// compiled facts, never through ingest dedup), so it is pinned here rather
// than assumed from the derivation shape.
func TestKubernetesCorrelationFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := KubernetesCorrelationFamilyOdu().Odu
	if len(odu.Facts) != 10 {
		t.Fatalf("kubernetes_correlation Odù carries %d facts, want 10 (3 OCI sources + 2 tag observations + 5 pod templates)", len(odu.Facts))
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
