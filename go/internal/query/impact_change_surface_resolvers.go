// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// changeSurfaceResolverGrantClause returns a WHERE fragment and augmented
// params binding the resolved node's repository grant property to the caller's
// grant, so the query's LIMIT applies to the GRANTED set instead of a
// cross-tenant-polluted pre-filter page (#5167 W3 P1 filter-before-limit). It
// returns ("", params unchanged) when the caller is not scoped, so a
// shared/admin/local caller's query is byte-identical to before.
func changeSurfaceResolverGrantClause(access repositoryAccessFilter, alias, grantProperty string, params map[string]any) (string, map[string]any) {
	if !access.scoped() {
		return "", params
	}
	return "\nWHERE " + access.graphConditionOnProperty(alias, grantProperty), access.graphParams(params)
}

func changeSurfaceResolverQueries(req changeSurfaceInvestigationRequest, limit int, access repositoryAccessFilter) []changeSurfaceResolverQuery {
	target := req.graphTarget()
	switch req.graphTargetType() {
	case "service", "workload":
		queries := []changeSurfaceResolverQuery{
			changeSurfaceWorkloadResolverQuery("id", target, 0, limit, access),
		}
		if canonicalID := canonicalWorkloadIDCandidate(target); canonicalID != target {
			queries = append(queries, changeSurfaceWorkloadResolverQuery("id", canonicalID, 1, limit, access))
		}
		if req.RepoID != "" {
			queries = append(queries, changeSurfaceWorkloadRepoScopedResolverQuery("name", target, req.RepoID, 2, limit, access))
		}
		queries = append(queries, changeSurfaceWorkloadResolverQuery("name", target, 3, limit, access))
		return queries
	case "workload_instance":
		return []changeSurfaceResolverQuery{
			changeSurfaceWorkloadInstanceResolverQuery("id", target, 0, limit, access),
			changeSurfaceWorkloadInstanceResolverQuery("workload_id", target, 1, limit, access),
		}
	case "repository", "repo":
		return []changeSurfaceResolverQuery{
			changeSurfaceRepositoryResolverQuery("id", target, 0, limit, access),
			changeSurfaceRepositoryResolverQuery("name", target, 1, limit, access),
		}
	case "resource", "cloud_resource":
		return []changeSurfaceResolverQuery{
			changeSurfaceCloudResourceResolverQuery("id", target, 0, limit, access),
			changeSurfaceCloudResourceResolverQuery("resource_id", target, 1, limit, access),
			changeSurfaceCloudResourceResolverQuery("name", target, 2, limit, access),
		}
	case "terraform_module", "module":
		return []changeSurfaceResolverQuery{
			changeSurfaceTerraformModuleResolverQuery("uid", target, 0, limit, access),
			changeSurfaceTerraformModuleResolverQuery("name", target, 1, limit, access),
		}
	default:
		return changeSurfaceGenericResolverQueries(target, limit, access)
	}
}

// changeSurfaceGenericResolverQueries builds the no-hint resolver probe order.
// Resolution breaks on the first probe that returns candidates, so probe order
// is resolution priority. Exact identity (id/uid) probes across every supported
// label run BEFORE any name fallback so a value that is one label's identity is
// never shadowed by another label's name match — this preserves the old
// `MATCH (start) WHERE start.id = $target_id` exact-id-first semantics that the
// label-anchored rewrite would otherwise drop (Codex P2 on #3384/#3388: a
// Repository id colliding with a Workload name resolved to the wrong node).
// Alternate identity keys (workload_id, resource_id) follow the primary keys;
// name probes run last.
func changeSurfaceGenericResolverQueries(target string, limit int, access repositoryAccessFilter) []changeSurfaceResolverQuery {
	rank := 0
	next := func() int { r := rank; rank++; return r }
	// Phase 1: primary identity (id/uid) across all supported labels.
	queries := []changeSurfaceResolverQuery{
		changeSurfaceWorkloadResolverQuery("id", target, next(), limit, access),
	}
	if canonicalID := canonicalWorkloadIDCandidate(target); canonicalID != target {
		queries = append(queries, changeSurfaceWorkloadResolverQuery("id", canonicalID, next(), limit, access))
	}
	queries = append(
		queries,
		changeSurfaceRepositoryResolverQuery("id", target, next(), limit, access),
		changeSurfaceWorkloadInstanceResolverQuery("id", target, next(), limit, access),
		changeSurfaceCloudResourceResolverQuery("id", target, next(), limit, access),
		changeSurfaceTerraformModuleResolverQuery("uid", target, next(), limit, access),
		changeSurfaceDataAssetResolverQuery("uid", target, next(), limit, access),
		// Phase 2: alternate identity keys.
		changeSurfaceWorkloadInstanceResolverQuery("workload_id", target, next(), limit, access),
		changeSurfaceCloudResourceResolverQuery("resource_id", target, next(), limit, access),
		// Phase 3: name fallbacks (lowest priority).
		changeSurfaceWorkloadResolverQuery("name", target, next(), limit, access),
		changeSurfaceRepositoryResolverQuery("name", target, next(), limit, access),
	)
	return queries
}

func canonicalWorkloadIDCandidate(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "workload:") {
		return target
	}
	return "workload:" + target
}

func changeSurfaceWorkloadResolverQuery(property string, target string, rank int, limit int, access repositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:Workload {%s: $target})%s
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceWorkloadRepoScopedResolverQuery(
	property string,
	target string,
	repoID string,
	rank int,
	limit int,
	access repositoryAccessFilter,
) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"repo_id": repoID, "target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:Workload {repo_id: $repo_id, %s: $target})%s
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceWorkloadInstanceResolverQuery(property string, target string, rank int, limit int, access repositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:WorkloadInstance {%s: $target})%s
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceRepositoryResolverQuery(property string, target string, rank int, limit int, access repositoryAccessFilter) changeSurfaceResolverQuery {
	// A Repository binds to the grant through its OWN id (repo_id == n.id here).
	where, params := changeSurfaceResolverGrantClause(access, "n", "id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:Repository {%s: $target})%s
RETURN n.id as id, n.name as name, labels(n) as labels, n.id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceCloudResourceResolverQuery(property string, target string, rank int, limit int, access repositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:CloudResource {%s: $target})%s
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceTerraformModuleResolverQuery(property string, target string, rank int, limit int, access repositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:TerraformModule {%s: $target})%s
RETURN n.uid as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceDataAssetResolverQuery(property string, target string, rank int, limit int, access repositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:DataAsset {%s: $target})%s
RETURN n.uid as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func appendChangeSurfaceCandidates(
	existing []changeSurfaceTargetCandidate,
	incoming []changeSurfaceTargetCandidate,
	limit int,
) []changeSurfaceTargetCandidate {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, candidate := range existing {
		seen[candidate.ID] = struct{}{}
	}
	for _, candidate := range incoming {
		if _, ok := seen[candidate.ID]; ok {
			continue
		}
		seen[candidate.ID] = struct{}{}
		existing = append(existing, candidate)
		if limit > 0 && len(existing) >= limit {
			return existing
		}
	}
	return existing
}

func changeSurfaceCandidates(rows []map[string]any) []changeSurfaceTargetCandidate {
	candidates := make([]changeSurfaceTargetCandidate, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidates = append(candidates, changeSurfaceTargetCandidate{
			ID:          id,
			Name:        StringVal(row, "name"),
			Labels:      StringSliceVal(row, "labels"),
			RepoID:      StringVal(row, "repo_id"),
			Environment: StringVal(row, "environment"),
			Rank:        IntVal(row, "rank"),
		})
	}
	return candidates
}

func changeSurfaceCandidateMaps(candidates []changeSurfaceTargetCandidate) []map[string]any {
	values := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Map())
	}
	return values
}

func (c changeSurfaceTargetCandidate) Map() map[string]any {
	value := map[string]any{"id": c.ID, "name": c.Name, "labels": c.Labels}
	if c.RepoID != "" {
		value["repo_id"] = c.RepoID
	}
	if c.Environment != "" {
		value["environment"] = c.Environment
	}
	return value
}
