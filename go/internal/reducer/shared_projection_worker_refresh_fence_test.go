// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// TestRepoWideRetractRefreshPartitionKeyShapeIsPinned pins the exact whole-scope
// refresh partition key string, because a cross-package test depends on that
// shape and cannot see this helper.
//
// storage/cypher's TestCollectDeltaAndWholeScopeRefreshRepoIDsStayDisjointAfterDedup
// proves that a repository's delta-flagged and non-delta whole-scope refresh
// rows collapse to one survivor before RetractEdges sees them, which is what
// keeps collectDeltaProjectionRepoIDs and collectWholeScopeRefreshRepoIDs
// disjoint. That proof rests on both rows sharing one partition key, and since
// repoWideRetractRefreshPartitionKey is unexported, that test mirrors the key as
// a hard-coded literal.
//
// A mirror is not a guard. If this scheme ever gained a delta or generation
// component, the mirrored literal would not change: both rows over there would
// still share the stale string, still collapse, and that test would stay green
// while proving nothing about the real disjointness -- the exact false-green the
// disjointness test exists to prevent. The existing assertions in
// rationale_edge_materialization_test.go and
// rationale_edge_materialization_partition_test.go cannot catch it either; they
// compare a generated key against the same generator, so they agree with
// themselves through any scheme change.
//
// This test is the guard that mirror needs. It compares the generator against a
// literal, so a scheme change fails HERE, in the package that owns the key,
// with a message naming the cross-package mirror that must be updated in
// lockstep (#6165 review, #6171).
func TestRepoWideRetractRefreshPartitionKeyShapeIsPinned(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repo-shape-pin"
		want   = "rationale_edges:refresh:v1:whole:repo-shape-pin"
	)

	if got := repoWideRetractRefreshPartitionKey(DomainRationaleEdges, repoID); got != want {
		t.Fatalf("repoWideRetractRefreshPartitionKey(%q, %q) = %q, want %q\n"+
			"The whole-scope refresh key scheme changed. storage/cypher's\n"+
			"edge_writer_retract_scope_test.go mirrors this exact string as a\n"+
			"literal to prove the delta and whole-scope retract collectors stay\n"+
			"disjoint; update that mirror in lockstep, or its rows will keep\n"+
			"sharing the stale key and collapse for the wrong reason.",
			DomainRationaleEdges, repoID, got, want)
	}
}

// TestRepoWideRetractRefreshPartitionKeyTrimsRepoID covers the whitespace
// handling the helper performs, so the pinned shape above is not read as the
// helper's only contract.
func TestRepoWideRetractRefreshPartitionKeyTrimsRepoID(t *testing.T) {
	t.Parallel()

	want := repoWideRetractRefreshPartitionKey(DomainRationaleEdges, "repo-1")
	if got := repoWideRetractRefreshPartitionKey(DomainRationaleEdges, "  repo-1\t"); got != want {
		t.Fatalf("padded repo id produced %q, want %q -- a padded id must not open a second partition for one repository", got, want)
	}
}
