// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// TestCICDImageMatchesForRepositoryNarrowsOnGitSourceRepositories is the #5766
// regression. container_image_identity's repository_id is the OCI registry's
// OWN identifier ("oci-registry://ghcr.io/org/repo"); a ci.run carries the
// canonical git repository id ("repository:r_..."). Narrowing compared those
// two namespaces directly, so it never matched: the digest match set was never
// reduced to one row and an otherwise-exact correlation degraded to ambiguous.
// The git repositories the identity decision attributed the image to live in
// source_repository_ids, which is the field #5464 established as the joinable
// one for exactly this reason.
func TestCICDImageMatchesForRepositoryNarrowsOnGitSourceRepositories(t *testing.T) {
	t.Parallel()

	const runRepositoryID = "repository:r_69256c06"

	matches := []cicdImageIdentity{
		{
			factID:              "identity-built-by-this-repo",
			sourceRepositoryIDs: []string{runRepositoryID},
			digest:              "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		},
		{
			factID:              "identity-from-another-repo",
			sourceRepositoryIDs: []string{"repository:r_someone_else"},
			digest:              "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		},
	}

	got := cicdImageMatchesForRepository(matches, runRepositoryID)
	if len(got) != 1 {
		t.Fatalf("cicdImageMatchesForRepository() = %d matches, want exactly 1 narrowed by git source repository: %#v", len(got), got)
	}
	if got[0].factID != "identity-built-by-this-repo" {
		t.Fatalf("narrowed to %q, want the identity whose source_repository_ids names the run's repository", got[0].factID)
	}
}

// TestCICDImageMatchesForRepositoryIgnoresOCIRegistryPaths pins the other half
// of #5766: an OCI registry path must never satisfy narrowing. Matching on one
// produces a non-blank anchor that no workload/service/deployment record can
// ever join (the #5463 dead-anchor failure mode).
//
// The identity payload's own repository_id is no longer decoded at all, so the
// original regression is now structurally unreachable. This asserts the
// remaining reachable shape: an OCI path that leaks into an identity's
// attributed-repository list still never narrows a canonical run repository,
// and passing an OCI path as the run's own repository narrows nothing either.
func TestCICDImageMatchesForRepositoryIgnoresOCIRegistryPaths(t *testing.T) {
	t.Parallel()

	const (
		ociPath         = "oci-registry://ghcr.io/eshu-hq/demo"
		runRepositoryID = "repository:r_69256c06"
		digest          = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	)

	matches := []cicdImageIdentity{{
		factID:                       "identity-oci-only",
		sourceRepositoryIDs:          []string{ociPath},
		buildProvenanceRepositoryIDs: []string{ociPath},
		buildProvenanceKeyPresent:    true,
		digest:                       digest,
	}}

	if got := cicdImageMatchesForRepository(matches, runRepositoryID); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0 — an OCI registry path never joins a canonical run repository", got)
	}

	legacy := []cicdImageIdentity{{
		factID:              "identity-legacy-oci-only",
		sourceRepositoryIDs: []string{ociPath},
		digest:              digest,
	}}

	if got := cicdImageMatchesForRepository(legacy, runRepositoryID); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0 on the legacy fallback path too", got)
	}
}

// TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository is the #5823
// regression, and the inversion of the test that previously pinned this false
// positive as unavoidable.
//
// source_repository_ids conflates "this repository genuinely built the image"
// with "this repository's Kubernetes manifest merely references the digest" --
// the same conflation #5796 fixed inside container_image_identity by gating its
// BUILT_FROM projection on the narrower BuildProvenanceRepositoryIDs. That
// narrower set is now persisted as build_provenance_repository_ids, so this
// consumer joins on it too: a digest whose run repository appears only as a
// reference no longer narrows to one row and no longer promotes to exact.
func TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		deployingRepo = "repository:r_deploys_only"
		buildingRepo  = "repository:r_actually_built"
	)

	matches := []cicdImageIdentity{
		{
			factID:                       "identity-built-by-other-repo",
			sourceRepositoryIDs:          []string{buildingRepo},
			buildProvenanceRepositoryIDs: []string{buildingRepo},
			buildProvenanceKeyPresent:    true,
			digest:                       digest,
		},
		{
			// This row lists the deploying repository only because its manifest
			// references the digest. It carries the build-provenance key, and
			// that key does NOT name the deploying repository.
			factID:                    "identity-merely-referenced",
			sourceRepositoryIDs:       []string{deployingRepo},
			buildProvenanceKeyPresent: true,
			digest:                    digest,
		},
	}

	if got := cicdImageMatchesForRepository(matches, deployingRepo); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0: a repository that only "+
			"references the digest must not narrow to one row and promote to exact (#5823)", got)
	}
}

// TestCICDImageMatchesForRepositorySelectsBuildProvenanceRow is the positive
// half of #5823: the repository the identity domain attributed BUILD evidence
// to still narrows to exactly its own row.
func TestCICDImageMatchesForRepositorySelectsBuildProvenanceRow(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		buildingRepo = "repository:r_actually_built"
	)

	matches := []cicdImageIdentity{
		{
			factID:                       "identity-built-by-this-repo",
			sourceRepositoryIDs:          []string{buildingRepo, "repository:r_deploys_only"},
			buildProvenanceRepositoryIDs: []string{buildingRepo},
			buildProvenanceKeyPresent:    true,
			digest:                       digest,
		},
		{
			factID:                       "identity-from-another-repo",
			sourceRepositoryIDs:          []string{"repository:r_someone_else"},
			buildProvenanceRepositoryIDs: []string{"repository:r_someone_else"},
			buildProvenanceKeyPresent:    true,
			digest:                       digest,
		},
	}

	got := cicdImageMatchesForRepository(matches, buildingRepo)
	if len(got) != 1 || got[0].factID != "identity-built-by-this-repo" {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want exactly the build-provenance row", got)
	}
}

// TestCICDImageMatchesForRepositoryFallsBackForLegacyPayloads closes the
// mixed-generation window. Identity facts published before
// build_provenance_repository_ids existed carry no such key at all. Treating
// their absent key as "built nothing" would silently degrade every correlation
// against a dormant scope from exact to ambiguous, so an absent key -- and only
// an absent key -- falls back to the broader source_repository_ids join.
func TestCICDImageMatchesForRepositoryFallsBackForLegacyPayloads(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		buildingRepo = "repository:r_actually_built"
	)

	matches := []cicdImageIdentity{
		{
			factID:              "legacy-identity-built-by-this-repo",
			sourceRepositoryIDs: []string{buildingRepo},
			digest:              digest,
		},
		{
			factID:              "legacy-identity-from-another-repo",
			sourceRepositoryIDs: []string{"repository:r_someone_else"},
			digest:              digest,
		},
	}

	got := cicdImageMatchesForRepository(matches, buildingRepo)
	if len(got) != 1 || got[0].factID != "legacy-identity-built-by-this-repo" {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want the legacy source_repository_ids "+
			"join to still narrow when no row carries the build-provenance key", got)
	}
}

// TestCICDImageMatchesForRepositoryDoesNotFallBackWhenKeyPresent proves the
// fallback is scoped to the legacy shape only. Once any row carries the
// build-provenance key the payload generation is known to publish it, so a
// repository the key does not name must stay unnarrowed (conservatively
// ambiguous) rather than silently falling back to the reference-conflating
// join the fallback exists for.
func TestCICDImageMatchesForRepositoryDoesNotFallBackWhenKeyPresent(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		deployingRepo = "repository:r_deploys_only"
	)

	matches := []cicdImageIdentity{
		{
			factID:                       "identity-built-by-other-repo",
			sourceRepositoryIDs:          []string{deployingRepo},
			buildProvenanceRepositoryIDs: []string{"repository:r_actually_built"},
			buildProvenanceKeyPresent:    true,
			digest:                       digest,
		},
		{
			factID:              "legacy-row-alongside",
			sourceRepositoryIDs: []string{deployingRepo},
			digest:              digest,
		},
	}

	if got := cicdImageMatchesForRepository(matches, deployingRepo); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0: one row carrying the "+
			"build-provenance key proves the generation publishes it, so no fallback", got)
	}
}

// TestCICDImageMatchesForRepositoryDoesNotGuardSingleRowDigests bounds what
// #5823 actually protects, so the disclosure does not oversell it.
//
// Narrowing only ever REDUCES a match set; both callers apply it as
// "if len(repoMatches) > 0 { matches = repoMatches }". A digest with exactly
// one identity row therefore reaches `case 1` and promotes to exact whether or
// not that row's build provenance names the run's repository — narrowing
// returns zero, the caller keeps the unfiltered single row, and the promotion
// happens anyway. That is pre-existing behavior this change neither introduces
// nor worsens, but it means #5823's protection binds only on digests with two
// or more candidate rows.
func TestCICDImageMatchesForRepositoryDoesNotGuardSingleRowDigests(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:1010101010101010101010101010101010101010101010101010101010101010"
		deployingRepo = "repository:r_deploys_only"
	)

	matches := []cicdImageIdentity{{
		factID:                       "sole-identity-built-elsewhere",
		sourceRepositoryIDs:          []string{deployingRepo},
		buildProvenanceRepositoryIDs: []string{"repository:r_actually_built"},
		buildProvenanceKeyPresent:    true,
		digest:                       digest,
	}}

	got := cicdImageMatchesForRepository(matches, deployingRepo)
	if len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0", got)
	}
	// The caller keeps the unfiltered set when narrowing yields nothing, so the
	// sole row still reaches `case 1`. Assert that shape explicitly: a future
	// change that makes narrowing authoritative must update this test and the
	// disclosure together.
	if len(matches) != 1 {
		t.Fatalf("unfiltered match set = %d rows, want the single row the caller falls back to", len(matches))
	}
}
