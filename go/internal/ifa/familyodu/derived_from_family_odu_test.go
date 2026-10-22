// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// TestDerivedFromFamilyOduStableKeysAreUnique pins that every fact in the
// derived_from Odù carries a distinct stable_fact_key (#6228 P1 lesson from
// iam_can_perform).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// manifest keys derive from the digest, so four distinct digests plus the
// repository, the Dockerfile and the Kubernetes reference cannot share one
// — but key narrowness is invisible to every other check in this family
// (the offline guard runs over the compiled facts, never through ingest
// dedup), so it is pinned here rather than assumed from the derivation
// shape.
func TestDerivedFromFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := DerivedFromFamilyOdu().Odu
	if len(odu.Facts) != 7 {
		t.Fatalf("derived_from Odù carries %d facts, want 7 (1 repository + 1 Dockerfile + 1 Kubernetes reference + 4 manifests)", len(odu.Facts))
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

// TestDerivedFromFamilyOduRepositoryFactCarriesLocalPath pins that the
// family's repository fact carries a non-empty local_path (#6228
// derived_from live leaf). The codegraph extractor falls back to
// local_path for the Repository.path it MERGEs, and Repository.path
// carries a global UNIQUE constraint: a second path-less repository in
// the same determinism cell fails the maintenance pass with a commit
// conflict the 30s write deadline then reports as a timeout. The failure
// is order-dependent (the first empty path wins), so only a pin here —
// not the green gate it happened to pass in — keeps the next family from
// re-learning it.
func TestDerivedFromFamilyOduRepositoryFactCarriesLocalPath(t *testing.T) {
	t.Parallel()
	odu := DerivedFromFamilyOdu().Odu
	found := false
	for _, env := range odu.Facts {
		if env.FactKind != factload.FactKindRepository {
			continue
		}
		found = true
		localPath, _ := env.Payload["local_path"].(string)
		if localPath == "" {
			t.Fatalf("repository fact carries an empty local_path: the canonical Repository.path falls back to it and collides with every other path-less repository under the global UNIQUE constraint")
		}
	}
	if !found {
		t.Fatal("derived_from Odù carries no repository fact")
	}
}
