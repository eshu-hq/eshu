// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

func TestWorkloadGrantAdmittedUnscopedAlwaysAdmits(t *testing.T) {
	t.Parallel()

	access := RepositoryAccessFilter{AllScopes: true}
	if !WorkloadGrantAdmitted(access, "", nil) {
		t.Fatal("WorkloadGrantAdmitted() = false, want true for unscoped caller")
	}
	if !WorkloadGrantAdmitted(access, "repo-out-of-grant", nil) {
		t.Fatal("WorkloadGrantAdmitted() = false, want true for unscoped caller regardless of repoID")
	}
}

func TestWorkloadGrantAdmittedDirectRepoID(t *testing.T) {
	t.Parallel()

	access := RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repo-a"},
		Allowed:              map[string]struct{}{"repo-a": {}},
	}
	if !WorkloadGrantAdmitted(access, "repo-a", nil) {
		t.Fatal("WorkloadGrantAdmitted() = false, want true when repoID is directly granted")
	}
	if WorkloadGrantAdmitted(access, "repo-b", nil) {
		t.Fatal("WorkloadGrantAdmitted() = true, want false when repoID is not granted and no defining repos match")
	}
}

func TestWorkloadGrantAdmittedDefiningRepoID(t *testing.T) {
	t.Parallel()

	access := RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repo-a"},
		Allowed:              map[string]struct{}{"repo-a": {}},
	}
	// Name-collision case: the workload's own materialized repo_id names an
	// ungranted repository, but a granted repository DEFINES it too.
	if !WorkloadGrantAdmitted(access, "repo-other", []string{"repo-z", "repo-a"}) {
		t.Fatal("WorkloadGrantAdmitted() = false, want true when a defining repository is granted")
	}
}

func TestWorkloadGrantAdmittedNoMatchDeniesEverything(t *testing.T) {
	t.Parallel()

	access := RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repo-a"},
		Allowed:              map[string]struct{}{"repo-a": {}},
	}
	if WorkloadGrantAdmitted(access, "repo-other", []string{"repo-z", "repo-y"}) {
		t.Fatal("WorkloadGrantAdmitted() = true, want false when neither repoID nor any defining repo is granted")
	}
	if WorkloadGrantAdmitted(access, "", nil) {
		t.Fatal("WorkloadGrantAdmitted() = true, want false for an empty repoID and no defining repos")
	}
}

func TestWorkloadGrantAdmittedScopedEmptyGrantDeniesAll(t *testing.T) {
	t.Parallel()

	// A caller scoped with no grants at all (RepositoryAccessFilter.Empty())
	// must never admit a read through this helper either -- callers gate on
	// access.Empty() earlier, but this proves the helper itself fails closed
	// if that gate is ever skipped.
	access := RepositoryAccessFilter{}
	if WorkloadGrantAdmitted(access, "repo-a", []string{"repo-b"}) {
		t.Fatal("WorkloadGrantAdmitted() = true, want false for a scoped caller with no grants")
	}
}
