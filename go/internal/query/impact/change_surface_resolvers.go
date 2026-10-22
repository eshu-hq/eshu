// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// changeSurfaceResolverGrantClause returns a WHERE fragment and augmented
// params binding the resolved node's repository grant property to the caller's
// grant, so the query's LIMIT applies to the GRANTED set instead of a
// cross-tenant-polluted pre-filter page (#5167 W3 P1 filter-before-limit). It
// returns ("", params unchanged) when the caller is not scoped, so a
// shared/admin/local caller's query is byte-identical to before.
func changeSurfaceResolverGrantClause(access querycontract.RepositoryAccessFilter, alias, grantProperty string, params map[string]any) (string, map[string]any) {
	if !access.Scoped() {
		return "", params
	}
	return "\nWHERE " + access.GraphConditionOnProperty(alias, grantProperty), access.GraphParams(params)
}

func changeSurfaceResolverQueries(req ChangeSurfaceInvestigationRequest, limit int, access querycontract.RepositoryAccessFilter) []changeSurfaceResolverQuery {
	target := req.graphTarget()
	switch req.graphTargetType() {
	case "service", "workload":
		// Ladder rung 1 is the exact node id above. Rung 2 below is the
		// handle ladder (compatibility doc section 1.4): the stripped bare
		// name matched within the caller grant. More than one admitted
		// same-name node fails closed downstream in resolveChangeSurfaceTarget,
		// which returns the ambiguous candidates with their repo_ids.
		//
		// The old phase-3 bare-name fallback is subsumed here on purpose: a
		// bare target's rung-2 probe is byte-identical to it, and a handle
		// target's old phase-3 probe (name = the prefixed handle) could never
		// match a node name, so it was a dead read.
		queries := []changeSurfaceResolverQuery{
			changeSurfaceWorkloadResolverQuery("id", target, 0, limit, access),
		}
		if bare := changeSurfaceWorkloadHandleBareName(target); bare != "" {
			queries = append(queries, changeSurfaceWorkloadResolverQuery("name", bare, 1, limit, access))
		}
		if req.RepoID != "" {
			queries = append(queries, changeSurfaceWorkloadRepoScopedResolverQuery("name", workloadid.StripWorkloadHandlePrefix(target), req.RepoID, 2, limit, access))
		}
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
// the workload handle-ladder name probe is the deliberate exception: it runs
// at phase 2 per the compatibility doc section 1.4, and the remaining name
// fallback runs last.
func changeSurfaceGenericResolverQueries(target string, limit int, access querycontract.RepositoryAccessFilter) []changeSurfaceResolverQuery {
	rank := 0
	next := func() int { r := rank; rank++; return r }
	// Phase 1: primary identity (id/uid) across all supported labels. These
	// stay ahead of every name probe so a value that is one label's identity
	// is never shadowed by another label's name match (#3384/#3388: a
	// Repository id colliding with a Workload name resolves to the
	// Repository).
	queries := []changeSurfaceResolverQuery{
		changeSurfaceWorkloadResolverQuery("id", target, next(), limit, access),
		changeSurfaceRepositoryResolverQuery("id", target, next(), limit, access),
		changeSurfaceWorkloadInstanceResolverQuery("id", target, next(), limit, access),
		changeSurfaceCloudResourceResolverQuery("id", target, next(), limit, access),
		changeSurfaceTerraformModuleResolverQuery("uid", target, next(), limit, access),
		changeSurfaceDataAssetResolverQuery("uid", target, next(), limit, access),
	}
	// Phase 2 is the workload handle ladder (compatibility doc section 1.4):
	// the stripped bare name matched within the caller grant. It sits after
	// every label's identity probe (so #3388 still holds) but ahead of the
	// alternate identity keys (so a bare name still resolves to the Workload
	// before an instance's parent pointer does, as the old canonical-id probe
	// did).
	//
	// The old phase-3 workload bare-name fallback is subsumed here on
	// purpose: a bare target's rung-2 probe is byte-identical to it, and a
	// handle target's old phase-3 probe (name = the prefixed handle) could
	// never match a node name, so it was a dead read. The repository name
	// fallback below stays: it belongs to another label.
	if bare := changeSurfaceWorkloadHandleBareName(target); bare != "" {
		queries = append(queries, changeSurfaceWorkloadResolverQuery("name", bare, next(), limit, access))
	}
	queries = append(
		queries,
		// Phase 3: alternate identity keys, then the remaining name fallback.
		changeSurfaceWorkloadInstanceResolverQuery("workload_id", target, next(), limit, access),
		changeSurfaceCloudResourceResolverQuery("resource_id", target, next(), limit, access),
		changeSurfaceRepositoryResolverQuery("name", target, next(), limit, access),
	)
	return queries
}

// changeSurfaceWorkloadHandleBareName returns the bare workload name the
// handle ladder matches on: the stripped handle for handle input, the target
// itself for a bare name, and empty for a full scoped id (rung 1 already
// matched it exactly) or blank input.
func changeSurfaceWorkloadHandleBareName(target string) string {
	switch workloadid.ClassifyWorkloadSelector(target) {
	case workloadid.SelectorKindHandle, workloadid.SelectorKindBare:
		return workloadid.StripWorkloadHandlePrefix(target)
	default:
		return ""
	}
}

func canonicalWorkloadIDCandidate(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "workload:") {
		return target
	}
	return "workload:" + target
}

func changeSurfaceWorkloadResolverQuery(property string, target string, rank int, limit int, access querycontract.RepositoryAccessFilter) changeSurfaceResolverQuery {
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
	access querycontract.RepositoryAccessFilter,
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

func changeSurfaceWorkloadInstanceResolverQuery(property string, target string, rank int, limit int, access querycontract.RepositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:WorkloadInstance {%s: $target})%s
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceRepositoryResolverQuery(property string, target string, rank int, limit int, access querycontract.RepositoryAccessFilter) changeSurfaceResolverQuery {
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

func changeSurfaceCloudResourceResolverQuery(property string, target string, rank int, limit int, access querycontract.RepositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	legacyKeyPredicate := "coalesce(n.resource_type, n.data_type, '') <> 'aws_appsync_api_key'"
	if where == "" {
		where = "\nWHERE " + legacyKeyPredicate
	} else {
		where += "\n  AND " + legacyKeyPredicate
	}
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:CloudResource {%s: $target})%s
RETURN n.id as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceTerraformModuleResolverQuery(property string, target string, rank int, limit int, access querycontract.RepositoryAccessFilter) changeSurfaceResolverQuery {
	where, params := changeSurfaceResolverGrantClause(access, "n", "repo_id", map[string]any{"target": target})
	return changeSurfaceResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:TerraformModule {%s: $target})%s
RETURN n.uid as id, n.name as name, labels(n) as labels, n.repo_id as repo_id, n.environment as environment, %d as rank
ORDER BY rank, name, id
LIMIT %d`, property, where, rank, limit),
		params: params,
	}
}

func changeSurfaceDataAssetResolverQuery(property string, target string, rank int, limit int, access querycontract.RepositoryAccessFilter) changeSurfaceResolverQuery {
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
	existing []ChangeSurfaceTargetCandidate,
	incoming []ChangeSurfaceTargetCandidate,
	limit int,
) []ChangeSurfaceTargetCandidate {
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

func changeSurfaceCandidates(rows []map[string]any) []ChangeSurfaceTargetCandidate {
	candidates := make([]ChangeSurfaceTargetCandidate, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		id := querycontract.StringVal(row, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidates = append(candidates, ChangeSurfaceTargetCandidate{
			ID:          id,
			Name:        querycontract.StringVal(row, "name"),
			Labels:      querycontract.StringSliceVal(row, "labels"),
			RepoID:      querycontract.StringVal(row, "repo_id"),
			Environment: querycontract.StringVal(row, "environment"),
			Rank:        querycontract.IntVal(row, "rank"),
		})
	}
	return candidates
}

func changeSurfaceCandidateMaps(candidates []ChangeSurfaceTargetCandidate) []map[string]any {
	values := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Map())
	}
	return values
}

func (c ChangeSurfaceTargetCandidate) Map() map[string]any {
	value := map[string]any{"id": c.ID, "name": c.Name, "labels": c.Labels}
	if c.RepoID != "" {
		value["repo_id"] = c.RepoID
	}
	if c.Environment != "" {
		value["environment"] = c.Environment
	}
	return value
}
