// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// ScopedWorkloadWhereClause appends the caller's workload grant predicate to a
// Workload-anchored WHERE clause. A scoped caller only matches workloads whose
// repo is in its grant (directly or through a DEFINES scope edge); an
// unscoped caller matches everything.
//
// It lives here rather than in package query because the impact
// handler-family subpackage (#6060 lane B2) scopes its workload selector
// reads and cannot import the root package back without an import cycle.
// Package query keeps a forwarding wrapper under the original name.
func ScopedWorkloadWhereClause(whereClause string, access RepositoryAccessFilter) string {
	if !access.Scoped() {
		return whereClause
	}
	return whereClause + `
			AND (
				w.repo_id IN $allowed_repository_ids
				OR w.repo_id IN $allowed_scope_ids
				OR EXISTS {
					MATCH (scopeRepo:Repository)-[:DEFINES]->(w)
					WHERE ` + access.GraphCondition("scopeRepo") + `
				}
			)`
}
