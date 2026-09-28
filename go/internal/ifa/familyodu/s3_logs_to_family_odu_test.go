// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestS3LogsToFamilyOduStableKeysAreUnique pins that every fact in the
// s3_logs_to Odù carries a distinct stable_fact_key (#6228 P1 lesson
// from iam_can_perform, repeated for iam_escalation and ec2_uses_profile).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// fixture varies bucket identity per posture (bucket_name, with the ARN-tail
// fallback where the collector emits no bucket_name), so the derivation
// carries every collector identity input the fixture varies. Key narrowness is
// invisible to every other check in this family (the offline guard runs over
// the compiled facts, never through ingest dedup), so it is pinned here
// rather than assumed from the derivation shape.
func TestS3LogsToFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := S3LogsToFamilyOdu().Odu
	if len(odu.Facts) != 13 {
		t.Fatalf("s3_logs_to Odù carries %d facts, want 13 (7 buckets + 6 postures)", len(odu.Facts))
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
