// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package routes

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// SplitRelationships divides relationship rows into callers and callees,
// stopping at the row limit and reporting whether rows were truncated.
func SplitRelationships(rows []map[string]any, limit int) ([]map[string]any, []map[string]any, bool) {
	callers := make([]map[string]any, 0)
	callees := make([]map[string]any, 0)
	truncated := false
	for _, row := range rows {
		if len(callers)+len(callees) >= limit {
			truncated = true
			break
		}
		item := map[string]any{
			"entity_id":  querycontract.StringVal(row, "entity_id"),
			"name":       querycontract.StringVal(row, "name"),
			"file_path":  querycontract.StringVal(row, "file_path"),
			"repo_id":    querycontract.StringVal(row, "repo_id"),
			"language":   querycontract.StringVal(row, "language"),
			"start_line": querycontract.IntVal(row, "start_line"),
			"end_line":   querycontract.IntVal(row, "end_line"),
			"depth":      querycontract.IntVal(row, "depth"),
		}
		if querycontract.StringVal(row, "direction") == "incoming" {
			callers = append(callers, item)
		} else {
			callees = append(callees, item)
		}
	}
	return callers, callees, truncated
}

// EmptyImpact returns the impact shape for a route with no handler edge.
func EmptyImpact() map[string]any {
	return map[string]any{
		"workloads":    []map[string]any{},
		"repositories": []map[string]any{},
	}
}

// MergeMaps unions two impact sets by id in order, bounded to the limit.
func MergeMaps(first []map[string]any, second []map[string]any, limit int) []map[string]any {
	capacity := len(first) + len(second)
	if capacity > limit {
		capacity = limit
	}
	merged := make([]map[string]any, 0, capacity)
	seen := make(map[string]struct{}, len(first)+len(second))
	for _, items := range [][]map[string]any{first, second} {
		for _, item := range items {
			id := querycontract.StringVal(item, "id")
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			merged = append(merged, item)
			if len(merged) >= limit {
				return merged
			}
		}
	}
	return merged
}

// AccessParams binds the caller's repository grant onto the read params
// for a scoped caller; an unscoped caller's params pass through unchanged.
func AccessParams(access querycontract.RepositoryAccessFilter, params map[string]any) map[string]any {
	if !access.Scoped() {
		return params
	}
	params["allowed_repository_ids"] = access.GrantedRepositoryIDs()
	params["allowed_scope_ids"] = access.GrantedScopeIDs()
	return params
}

// EndpointAccessPredicate scopes the endpoint half of a route read to the
// caller's grant.
func EndpointAccessPredicate(access querycontract.RepositoryAccessFilter) string {
	if !access.Scoped() {
		return ""
	}
	return "(endpoint.repo_id IN $allowed_repository_ids OR endpoint.scope_id IN $allowed_scope_ids)"
}

// EntityAccessPredicate scopes a CALLS far-endpoint alias to the caller's
// grant.
func EntityAccessPredicate(access querycontract.RepositoryAccessFilter, alias string) string {
	if !access.Scoped() {
		return ""
	}
	return " AND (" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}

// PathAccessPredicate scopes every node of a CALLS path to the caller's
// grant.
func PathAccessPredicate(access querycontract.RepositoryAccessFilter, pathAlias string) string {
	if !access.Scoped() {
		return ""
	}
	return " AND all(pathNode IN nodes(" + pathAlias + ") WHERE " +
		"(pathNode.repo_id IN $allowed_repository_ids OR pathNode.scope_id IN $allowed_scope_ids))"
}

// RequiredNodeAccessClause renders the scoped-access predicate for
// a required (non-OPTIONAL) node match, ANDed onto the branch WHERE. Unlike the
// OPTIONAL form it has no `alias IS NULL OR` escape: an out-of-grant node is
// excluded (fail-closed), which is the correct scoped behavior for the split
// impact set reads.
func RequiredNodeAccessClause(access querycontract.RepositoryAccessFilter, alias string) string {
	if !access.Scoped() {
		return ""
	}
	return " AND (" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}

// RequiredRepositoryAccessClause is the required-match access
// predicate for a Repository node, whose grant identity is its own id.
func RequiredRepositoryAccessClause(access querycontract.RepositoryAccessFilter, alias string) string {
	if !access.Scoped() {
		return ""
	}
	return " AND (" + alias + ".id IN $allowed_repository_ids OR " + alias + ".scope_id IN $allowed_scope_ids)"
}
