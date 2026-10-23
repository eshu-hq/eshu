// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestCodeTaintEvidenceFamilyOduStableKeysAreUnique pins that every fact in
// the code_taint_evidence Odù carries a distinct stable_fact_key (#6228 P1
// lesson from iam_can_perform, repeated for iam_escalation,
// ec2_uses_profile, and s3_logs_to).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// fixture keys each finding by endpoint uid plus lines plus kinds plus
// binding, and each substrate fact by its own identity input, so the
// derivation carries every collector identity input the fixture varies. Key
// narrowness is invisible to every other check in this family (the offline
// guard runs over the compiled facts, never through ingest dedup), so it is
// pinned here rather than assumed from the derivation shape.
func TestCodeTaintEvidenceFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := CodeTaintEvidenceFamilyOdu().Odu
	if len(odu.Facts) != 8 {
		t.Fatalf("code_taint_evidence Odù carries %d facts, want 8 (1 repository + 2 files + 2 content entities + 2 live findings + 1 tombstoned finding)", len(odu.Facts))
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

// TestCodeTaintEvidenceFamilyOduRepositoryFactCarriesLocalPath pins that the
// family's repository fact carries a non-empty local_path (#6228
// code_taint_evidence live leaf). The codegraph extractor falls back to
// local_path for the Repository.path it MERGEs, and Repository.path
// carries a global UNIQUE constraint: a second path-less repository in
// the same determinism cell fails the maintenance pass with a commit
// conflict the 30s write deadline then reports as a timeout. The failure
// is order-dependent (the first empty path wins), so only a pin here --
// not the green gate it happened to pass in -- keeps the next family from
// re-learning it.
func TestCodeTaintEvidenceFamilyOduRepositoryFactCarriesLocalPath(t *testing.T) {
	t.Parallel()
	odu := CodeTaintEvidenceFamilyOdu().Odu
	found := false
	for _, env := range odu.Facts {
		if env.FactKind != RepositoryFactKind {
			continue
		}
		found = true
		localPath, _ := env.Payload["local_path"].(string)
		if localPath == "" {
			t.Fatalf("repository fact carries an empty local_path: the canonical Repository.path falls back to it and collides with every other path-less repository under the global UNIQUE constraint")
		}
	}
	if !found {
		t.Fatal("code_taint_evidence Odù carries no repository fact")
	}
}
