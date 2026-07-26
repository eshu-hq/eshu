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
			repositoryID:        "oci-registry://ghcr.io/eshu-hq/demo",
			sourceRepositoryIDs: []string{runRepositoryID},
			digest:              "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		},
		{
			factID:              "identity-from-another-repo",
			repositoryID:        "oci-registry://ghcr.io/eshu-hq/demo",
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

// TestCICDImageMatchesForRepositoryIgnoresOCIRegistryRepositoryID pins the
// other half of #5766: an OCI registry path must never satisfy narrowing, even
// when it is byte-equal to the value passed in. Matching on it produced a
// non-blank anchor that no workload/service/deployment record can ever join
// (the #5463 dead-anchor failure mode).
func TestCICDImageMatchesForRepositoryIgnoresOCIRegistryRepositoryID(t *testing.T) {
	t.Parallel()

	const ociPath = "oci-registry://ghcr.io/eshu-hq/demo"

	matches := []cicdImageIdentity{{
		factID:       "identity-oci-only",
		repositoryID: ociPath,
		digest:       "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}}

	if got := cicdImageMatchesForRepository(matches, ociPath); len(got) != 0 {
		t.Fatalf("cicdImageMatchesForRepository() = %d matches, want 0 — an OCI registry path is not a joinable git anchor: %#v", len(got), got)
	}
}

// TestCICDImageMatchesForRepositoryCannotDistinguishBuiltFromReferenced pins the
// residual accuracy risk #5796 closed for the sibling container_image_identity
// domain but which this consumer structurally cannot close today.
//
// #5796 stopped that domain projecting BUILT_FROM from SourceRepositoryIDs,
// because that field conflates "this repository genuinely built the image" with
// "this repository's Kubernetes manifest merely references the digest". It
// gated the projection on the narrower BuildProvenanceRepositoryIDs instead.
// That field is NOT persisted: containerImageIdentityPayload writes only
// source_repository_ids, so a cross-scope consumer like this one can only join
// on the broad field.
//
// The consequence is real and is asserted here rather than left implicit: when a
// digest has two candidate identities and the run's repository appears ONLY as a
// reference on the second, narrowing selects that second row, the correlation
// promotes to exact, and the projection emits BUILT_FROM for a repository that
// merely deploys the image. This test documents the current behavior so the fix
// (persisting build_provenance_repository_ids and narrowing on it, #5823) has a
// failing-then-green target and cannot land silently.
func TestCICDImageMatchesForRepositoryCannotDistinguishBuiltFromReferenced(t *testing.T) {
	t.Parallel()

	const (
		digest        = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		deployingRepo = "repository:r_deploys_only"
		buildingRepo  = "repository:r_actually_built"
	)

	matches := []cicdImageIdentity{
		{
			factID:              "identity-built-by-other-repo",
			sourceRepositoryIDs: []string{buildingRepo},
			digest:              digest,
		},
		{
			// This row lists the deploying repository only because its manifest
			// references the digest -- it did not build the image. Nothing in the
			// persisted payload distinguishes that from a real build.
			factID:              "identity-merely-referenced",
			sourceRepositoryIDs: []string{deployingRepo},
			digest:              digest,
		},
	}

	got := cicdImageMatchesForRepository(matches, deployingRepo)
	if len(got) != 1 || got[0].factID != "identity-merely-referenced" {
		t.Fatalf("cicdImageMatchesForRepository() = %#v, want the reference-only row selected; "+
			"if this now returns 0 matches, build-provenance narrowing has landed and this "+
			"test should be inverted to assert the false positive is gone (#5823)", got)
	}
}
