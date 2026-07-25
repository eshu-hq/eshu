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
