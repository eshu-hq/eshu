// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"net/http"
)

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

// WorkloadSelectorCandidateBound caps how many Workload rows a name-keyed
// selector read fetches before its grant, ambiguity, and pick decisions run
// in Go. Readers fetch WorkloadSelectorCandidateBound+1 rows so a full page
// is distinguishable from an exact one.
//
// Workload names are not unique. For a scoped caller the name read carries
// WorkloadScopePredicate on its WHERE line (#6801), so the bound counts granted
// rows; unscoped reads count every same-name row. Up to the bound every row is
// inspected; past it, a matching row may be missing, so callers fail closed
// with ErrWorkloadSelectorCandidatesExceedBound instead of deciding from a
// truncated page.
const WorkloadSelectorCandidateBound = 50

// ErrWorkloadSelectorCandidatesExceedBound reports that a name-keyed Workload
// selector matched more rows than WorkloadSelectorCandidateBound. HTTP
// handlers map it to 409 Conflict with a fixed message that tells the caller
// to retry with a workload id. The error text never carries the row count.
// For a scoped caller the name read carries WorkloadScopePredicate, so the
// bound counts granted rows only; the text stays count-free for unscoped
// callers, and as defense in depth in case a backend ignores the predicate.
var ErrWorkloadSelectorCandidatesExceedBound = errors.New("workload selector matched more candidates than can be resolved; retry with a workload id")

// WriteWorkloadSelectorOverflow writes the 409 Conflict response for
// ErrWorkloadSelectorCandidatesExceedBound and reports whether err matched.
// It writes the sentinel's own fixed text, not err.Error(), so a caller that
// wrapped the sentinel with extra context cannot leak that context either.
// 409 matches how the same routes report an ambiguous selector: the caller
// can resolve it by retrying with a workload id.
func WriteWorkloadSelectorOverflow(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, ErrWorkloadSelectorCandidatesExceedBound) {
		return false
	}
	WriteError(w, http.StatusConflict, ErrWorkloadSelectorCandidatesExceedBound.Error())
	return true
}
