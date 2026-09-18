// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// WorkloadGrantAdmitted reports whether a scoped caller's grant admits a
// Workload read whose materialized repo_id is repoID and whose DEFINES-linked
// repositories are definingRepoIDs. An unscoped caller admits unconditionally.
//
// This is the Go-side successor to the Cypher-embedded grant predicate this
// package used to render (the retired ScopedWorkloadWhereClause): on the
// pinned NornicDB v1.3.3 image, a multi-line `AND ( ... OR EXISTS { ... } )`
// WHERE group is unreliable -- the whole WHERE, including an unrelated
// `w.id = $id` anchor on the same MATCH, can be silently dropped, which let a
// scoped caller read an arbitrary Workload regardless of grant (#6786). A
// single-line `IN` disjunction (querycontract.WorkloadScopePredicate,
// SHAPE-A) still renders correctly, so that predicate stays; only the
// multi-line EXISTS form is retired in favor of deciding the grant here.
//
// A workload is admitted through either of two routes:
//
//  1. direct ownership: the workload's materialized repo_id is granted, or
//  2. DEFINES admission: a granted Repository DEFINES the workload. This
//     covers a name-collision workload whose materialized repo_id names only
//     ONE of the repositories that define it -- a grant for the OTHER
//     defining repository is missed by the direct check alone.
//
// Callers resolve definingRepoIDs from their own DEFINES read (a bounded,
// single-line-WHERE query already proven safe on NornicDB) rather than this
// function issuing one, so the decision stays a pure function of rows the
// caller already fetched.
func WorkloadGrantAdmitted(access RepositoryAccessFilter, repoID string, definingRepoIDs []string) bool {
	if !access.Scoped() {
		return true
	}
	if access.AllowsRepositoryID(repoID) {
		return true
	}
	for _, definingRepoID := range definingRepoIDs {
		if access.AllowsRepositoryID(definingRepoID) {
			return true
		}
	}
	return false
}
