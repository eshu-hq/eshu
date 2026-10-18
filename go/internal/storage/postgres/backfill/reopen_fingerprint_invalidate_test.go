// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"strings"
	"testing"
)

// TestInvalidateDeployableUnitReopenFingerprintsForRepoQueryDeletesByRepoMembership
// pins the #8316 single-repo invalidation predicate: deleting the fingerprint
// rows for an item when a Repository deleter removes a node, in the exact
// form the issue names.
func TestInvalidateDeployableUnitReopenFingerprintsForRepoQueryDeletesByRepoMembership(t *testing.T) {
	t.Parallel()

	want := `DELETE FROM deployable_unit_reopen_fingerprints WHERE $1 = ANY(repo_ids)`
	if InvalidateDeployableUnitReopenFingerprintsForRepoQuery != want {
		t.Fatalf("single-repo invalidate query =\n%q\nwant\n%q",
			InvalidateDeployableUnitReopenFingerprintsForRepoQuery, want)
	}
}

// TestInvalidateDeployableUnitReopenFingerprintsForReposQueryDeletesOverlapBatch
// pins the batch invalidation predicate: one statement for every repository
// id a sweep cycle deleted, matched by array overlap.
func TestInvalidateDeployableUnitReopenFingerprintsForReposQueryDeletesOverlapBatch(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"DELETE FROM deployable_unit_reopen_fingerprints",
		"repo_ids && $1",
	} {
		if !strings.Contains(InvalidateDeployableUnitReopenFingerprintsForReposQuery, fragment) {
			t.Fatalf("batch invalidate query missing %q:\n%s",
				fragment, InvalidateDeployableUnitReopenFingerprintsForReposQuery)
		}
	}
}

// TestListAllDeployableUnitAdmittedAnchorReposQueryIsFullAdmittedScan pins
// the #8316 gap-reconciliation Postgres half: the full distinct source
// repository set with admitted deployable_unit_correlation decisions, with
// no LIMIT — a first-N prefix would starve tail anchors (#8371 review F1).
// The graph probe batches the set into fixed-size UNWINDs instead.
func TestListAllDeployableUnitAdmittedAnchorReposQueryIsFullAdmittedScan(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"DISTINCT anchor_id",
		"FROM admission_decisions",
		"domain = 'deployable_unit_correlation'",
		"state = 'admitted'",
		"anchor_kind = 'repository'",
		"ORDER BY anchor_id",
	} {
		if !strings.Contains(ListAllDeployableUnitAdmittedAnchorReposQuery, fragment) {
			t.Fatalf("admitted anchors query missing %q:\n%s",
				fragment, ListAllDeployableUnitAdmittedAnchorReposQuery)
		}
	}
	if strings.Contains(ListAllDeployableUnitAdmittedAnchorReposQuery, "LIMIT") {
		t.Fatalf("admitted anchors query must carry no LIMIT:\\n%s",
			ListAllDeployableUnitAdmittedAnchorReposQuery)
	}
}
