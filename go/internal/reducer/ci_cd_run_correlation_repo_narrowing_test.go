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
// The joinable anchor is the identity's own attributed git repositories, which
// this consumer now reads from build_provenance_repository_ids (#5823); the
// broad source_repository_ids field is not decoded here at all.
func TestCICDImageMatchesForRepositoryNarrowsOnGitSourceRepositories(t *testing.T) {
	t.Parallel()

	const runRepositoryID = "repository:r_69256c06"

	matches := []cicdImageIdentity{
		{
			factID:                       "identity-built-by-this-repo",
			buildProvenanceRepositoryIDs: []string{runRepositoryID},
			digest:                       "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		},
		{
			factID:                       "identity-from-another-repo",
			buildProvenanceRepositoryIDs: []string{"repository:r_someone_else"},
			digest:                       "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		},
	}

	got := cicdImageMatchesForRepository(matches, runRepositoryID)
	if len(got) != 1 {
		t.Fatalf("cicdImageMatchesForRepository() = %d matches, want exactly 1 narrowed by git source repository: %#v", len(got), got)
	}
	if got[0].factID != "identity-built-by-this-repo" {
		t.Fatalf("narrowed to %q, want the identity whose build provenance names the run's repository", got[0].factID)
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
		buildProvenanceRepositoryIDs: []string{ociPath},
		digest:                       digest,
	}}

	if got := cicdImageMatchesForRepository(matches, runRepositoryID); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0 — an OCI registry path never joins a canonical run repository", got)
	}

	legacy := []cicdImageIdentity{{
		factID: "identity-legacy-oci-only",
		digest: digest,
	}}

	if got := cicdImageMatchesForRepository(legacy, runRepositoryID); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0 on the legacy fallback path too", got)
	}
}

// TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository is the #5823
// regression, and the inversion of the test that previously pinned this false
// positive as unavoidable.
//
// The published source_repository_ids field conflates "this repository
// genuinely built the image" with "this repository's Kubernetes manifest merely
// references the digest" --
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
			buildProvenanceRepositoryIDs: []string{buildingRepo},
			digest:                       digest,
		},
		{
			// This row lists the deploying repository only because its manifest
			// references the digest, so its build provenance does not name that
			// repository. A nil slice here is the same input a row published
			// before the key existed produces; narrowing treats the two
			// identically, which is why neither is ever selected.
			factID: "identity-merely-referenced",
			digest: digest,
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
			buildProvenanceRepositoryIDs: []string{buildingRepo},
			digest:                       digest,
		},
		{
			factID:                       "identity-from-another-repo",
			buildProvenanceRepositoryIDs: []string{"repository:r_someone_else"},
			digest:                       digest,
		},
	}

	got := cicdImageMatchesForRepository(matches, buildingRepo)
	if len(got) != 1 || got[0].factID != "identity-built-by-this-repo" {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want exactly the build-provenance row", got)
	}
}

// TestCICDImageMatchesForRepositoryNeverSelectsLegacyRows is the #5823
// correction that the final review forced.
//
// An earlier revision of this change fell back to the broad
// source_repository_ids join whenever no candidate row declared the
// build-provenance key, on the theory that treating an absent key as "built
// nothing" would degrade correlations against scopes that had not republished.
// A legacy row is expressed here as one carrying no build provenance; the
// payload-level shape, including the reference-only source_repository_ids it
// still publishes, is covered by
// TestCICDNarrowingSelectsTheBuilderFromPublishedFacts.
// That theory was wrong, and the fallback was an accuracy regression.
//
// Before #5766 the predicate compared the identity's OCI repository_id against
// a canonical repository:r_... id, so narrowing was a dead no-op and every
// legacy multi-row digest ALREADY resolved ambiguous. There is no lost
// correlation for a fallback to recover. All the fallback could do is select a
// reference-only legacy row and manufacture an exact that the previous behavior
// never produced.
//
// So legacy rows are simply never selected. The sharper join engages for a
// scope as soon as its identity intent republishes with the key.
func TestCICDImageMatchesForRepositoryNeverSelectsLegacyRows(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		buildingRepo  = "repository:r_actually_built"
		deployingRepo = "repository:r_deploys_only"
	)

	legacy := []cicdImageIdentity{
		{
			factID: "legacy-identity-built-by-this-repo",
			digest: digest,
		},
		{
			factID: "legacy-identity-from-another-repo",
			digest: digest,
		},
	}

	// The builder itself is not selected either. The caller keeps the
	// unfiltered set and resolves ambiguous, which is exactly what origin/main
	// does for this input.
	if got := cicdImageMatchesForRepository(legacy, buildingRepo); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0: a row published without "+
			"build_provenance_repository_ids carries no build evidence to join on", got)
	}

	// The failure this protects against: a legacy row naming a repository that
	// only DEPLOYS the image must never narrow the set to one and promote that
	// repository to exact.
	referenceOnly := []cicdImageIdentity{
		{
			factID: "legacy-identity-built-elsewhere",
			digest: digest,
		},
		{
			factID: "legacy-identity-merely-referenced",
			digest: digest,
		},
	}

	if got := cicdImageMatchesForRepository(referenceOnly, deployingRepo); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0: narrowing a legacy "+
			"reference-only row to a single match would promote a deploy-only repository "+
			"to exact, an outcome the pre-#5766 behavior never produced (#5823)", got)
	}
}

// TestCICDImageMatchesForRepositoryIgnoresLegacyRowsBesideCurrentOnes pins that
// the two generations do not contaminate each other: a current row's published
// build provenance decides selection, and a legacy sibling for the same digest
// neither adds nor blocks a match.
func TestCICDImageMatchesForRepositoryIgnoresLegacyRowsBesideCurrentOnes(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		buildingRepo  = "repository:r_actually_built"
		deployingRepo = "repository:r_deploys_only"
	)

	matches := []cicdImageIdentity{
		{
			factID:                       "current-identity-built-here",
			buildProvenanceRepositoryIDs: []string{buildingRepo},
			digest:                       digest,
		},
		{
			factID: "legacy-identity-referenced-by-deployer",
			digest: digest,
		},
	}

	got := cicdImageMatchesForRepository(matches, buildingRepo)
	if len(got) != 1 || got[0].factID != "current-identity-built-here" {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want only the current build-provenance row", got)
	}

	if got := cicdImageMatchesForRepository(matches, deployingRepo); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want 0 for the deploy-only repository", got)
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
		buildProvenanceRepositoryIDs: []string{"repository:r_actually_built"},
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
