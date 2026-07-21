// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/lib/pq"
)

// TestDeferredCatalogFingerprintStable asserts the fingerprint is a pure,
// deterministic function of the two catalog-derived arrays: the same arrays,
// even when supplied in different orders (simulating non-deterministic
// upstream map/slice iteration), always produce the same digest.
func TestDeferredCatalogFingerprintStable(t *testing.T) {
	t.Parallel()

	a := deferredScopedFactQueryParams{
		nonRepoIDLike: pq.StringArray{"%alpha%", "%beta%", "%gamma%"},
		repoIDValues:  pq.StringArray{"repo-a", "repo-b", "repo-c"},
	}
	// Same content, different order.
	b := deferredScopedFactQueryParams{
		nonRepoIDLike: pq.StringArray{"%gamma%", "%alpha%", "%beta%"},
		repoIDValues:  pq.StringArray{"repo-c", "repo-a", "repo-b"},
	}

	fpA := deferredCatalogFingerprint(a)
	fpB := deferredCatalogFingerprint(b)
	if fpA != fpB {
		t.Fatalf("fingerprint is not order-insensitive: fpA=%q fpB=%q", fpA, fpB)
	}
	if fpA == "" {
		t.Fatal("fingerprint must not be empty for a non-empty catalog")
	}
}

// TestDeferredCatalogFingerprintChangesOnCatalogEdit proves the fingerprint
// flips whenever ANY element is added, removed, or renamed in either array —
// the exact invalidation signal a repo onboard/rename/remove must produce (see
// the design note in ingestion_backfill_partition_memo_fingerprint.go). Each
// subtest mutates exactly one element relative to a shared baseline.
func TestDeferredCatalogFingerprintChangesOnCatalogEdit(t *testing.T) {
	t.Parallel()

	baseline := deferredScopedFactQueryParams{
		nonRepoIDLike: pq.StringArray{"%alpha%", "%beta%"},
		repoIDValues:  pq.StringArray{"repo-a", "repo-b"},
		remoteURLs:    pq.StringArray{"https://github.com/acme/alpha", "https://github.com/acme/beta"},
	}
	baselineFP := deferredCatalogFingerprint(baseline)

	cases := map[string]deferredScopedFactQueryParams{
		"added_non_repo_id_term": {
			nonRepoIDLike: pq.StringArray{"%alpha%", "%beta%", "%gamma%"},
			repoIDValues:  pq.StringArray{"repo-a", "repo-b"},
			remoteURLs:    pq.StringArray{"https://github.com/acme/alpha", "https://github.com/acme/beta"},
		},
		"removed_non_repo_id_term": {
			nonRepoIDLike: pq.StringArray{"%alpha%"},
			repoIDValues:  pq.StringArray{"repo-a", "repo-b"},
			remoteURLs:    pq.StringArray{"https://github.com/acme/alpha", "https://github.com/acme/beta"},
		},
		"added_repo_id_value": {
			nonRepoIDLike: pq.StringArray{"%alpha%", "%beta%"},
			repoIDValues:  pq.StringArray{"repo-a", "repo-b", "repo-c"},
			remoteURLs:    pq.StringArray{"https://github.com/acme/alpha", "https://github.com/acme/beta"},
		},
		"removed_repo_id_value": {
			nonRepoIDLike: pq.StringArray{"%alpha%", "%beta%"},
			repoIDValues:  pq.StringArray{"repo-a"},
			remoteURLs:    pq.StringArray{"https://github.com/acme/alpha", "https://github.com/acme/beta"},
		},
		"renamed_repo_id_value": {
			nonRepoIDLike: pq.StringArray{"%alpha%", "%beta%"},
			repoIDValues:  pq.StringArray{"repo-a", "repo-b-renamed"},
			remoteURLs:    pq.StringArray{"https://github.com/acme/alpha", "https://github.com/acme/beta"},
		},
		// #5483 C2: a repository's remote_url change (a mirror migration) moves
		// neither the alias LIKE terms nor the repo_id values, but MUST still
		// flip the fingerprint so the strict Flux cross-repo resolver's
		// changed input re-triggers deferred re-discovery.
		"changed_remote_url": {
			nonRepoIDLike: pq.StringArray{"%alpha%", "%beta%"},
			repoIDValues:  pq.StringArray{"repo-a", "repo-b"},
			remoteURLs:    pq.StringArray{"https://github.com/acme/alpha", "https://gitlab.example.com/acme/beta"},
		},
	}

	for name, mutated := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fp := deferredCatalogFingerprint(mutated)
			if fp == baselineFP {
				t.Fatalf("mutation %q did not change the fingerprint (baseline=%q got=%q)", name, baselineFP, fp)
			}
		})
	}
}

// TestDeferredCatalogFingerprintEmptyParams proves an empty catalog still
// produces a stable, non-empty fingerprint distinct from a non-empty one, so
// bootstrap (no memo rows, but also potentially no anchors) never collides with
// a real catalog's digest.
func TestDeferredCatalogFingerprintEmptyParams(t *testing.T) {
	t.Parallel()

	empty := deferredScopedFactQueryParams{}
	fp := deferredCatalogFingerprint(empty)
	if fp == "" {
		t.Fatal("fingerprint must not be empty even for empty params")
	}

	nonEmpty := deferredScopedFactQueryParams{
		repoIDValues: pq.StringArray{"repo-a"},
	}
	if deferredCatalogFingerprint(nonEmpty) == fp {
		t.Fatal("empty and non-empty params must not collide")
	}
}
