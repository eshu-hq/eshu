// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// TestPublishesFamilyOduStableKeysAreUnique pins that every fact in the
// publishes Odù carries a distinct stable_fact_key (#6228 P1 lesson from
// iam_can_perform).
//
// Cassette facts carry no fact_id: replay derives FactID from (scope,
// generation, stable_fact_key) and ingest upserts ON CONFLICT (fact_id), so
// two facts sharing a key collapse last-writer-wins at live ingest. The
// hint keys derive from package, version, kind and URL, the version keys
// from the version id, and the repository keys from the repository id —
// but key narrowness is invisible to every other check in this family (the
// offline guard runs over the compiled facts, never through ingest dedup),
// so it is pinned here rather than assumed from the derivation shape.
func TestPublishesFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()
	odu := PublishesFamilyOdu().Odu
	if len(odu.Facts) != 13 {
		t.Fatalf("publishes Odù carries %d facts, want 13 (4 repositories + 6 hints + 2 versions + 1 package)", len(odu.Facts))
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

// TestPublishesFamilyOduRepositoryFactsCarrySourceRunID pins the canonical
// projection identity contract (#6228 publishes live leaf): the canonical
// repository phase drops a repository fact whose source_run_id is empty
// ("missing identity"), so without it the fixture's Repository nodes never
// commit and the provenance writer's MATCH finds nothing live. The offline
// guard cannot catch the omission (neither correlation builder reads
// source_run_id), so it is pinned here rather than assumed from the
// collector payload shape.
func TestPublishesFamilyOduRepositoryFactsCarrySourceRunID(t *testing.T) {
	t.Parallel()
	odu := PublishesFamilyOdu().Odu
	repositories := 0
	for _, env := range odu.Facts {
		if env.FactKind != factload.FactKindRepository {
			continue
		}
		repositories++
		runID, _ := env.Payload["source_run_id"].(string)
		if runID == "" {
			t.Fatalf("repository fact %q carries no source_run_id: canonical projection would drop it as identity-less", env.StableFactKey)
		}
	}
	if repositories != 4 {
		t.Fatalf("publishes Odù carries %d repository facts, want 4", repositories)
	}
}
