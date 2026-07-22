// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

func (h *ImpactHandler) resolveEntityMapStart(
	ctx context.Context,
	req entityMapRequest,
) (*entityMapCandidate, map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil, fmt.Errorf("graph backend is unavailable")
	}
	candidates := make([]entityMapCandidate, 0, req.Limit+1)
	for _, query := range entityMapResolverQueries(req, req.Limit+1) {
		query.params["environment"] = req.Environment
		query.params["repo_id"] = req.RepoID
		rows, err := h.Neo4j.Run(ctx, query.cypher, query.params)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve entity map start: %w", err)
		}
		candidates = appendEntityMapCandidates(candidates, entityMapCandidates(rows), req.Limit+1)
		if len(candidates) > 0 {
			break
		}
	}
	totalCandidates := len(candidates)
	truncated := len(candidates) > req.Limit
	if truncated {
		candidates = candidates[:req.Limit]
	}
	resolution := map[string]any{
		"input":      req.responseFrom(),
		"normalized": req.From,
		"from_type":  req.FromType,
		"status":     "no_match",
		"candidates": entityMapCandidateMaps(candidates),
		"truncated":  truncated,
	}
	switch totalCandidates {
	case 0:
		return nil, resolution, nil
	case 1:
		resolution["status"] = "resolved"
		resolution["selected"] = candidates[0].Map()
		return &candidates[0], resolution, nil
	default:
		resolution["status"] = "ambiguous"
		return nil, resolution, nil
	}
}

func entityMapResolverQueries(req entityMapRequest, limit int) []entityMapResolverQuery {
	from := req.From
	switch req.FromType {
	case "service", "workload":
		queries := []entityMapResolverQuery{entityMapNodeResolverQuery("Workload", "id", from, "id", 0, limit)}
		if canonicalID := canonicalWorkloadIDCandidate(from); canonicalID != from {
			queries = append(queries, entityMapNodeResolverQuery("Workload", "id", canonicalID, "id", 1, limit))
		}
		if req.RepoID != "" {
			queries = append(queries, entityMapWorkloadRepoScopedResolverQuery(from, req.RepoID, 2, limit))
		}
		queries = append(queries, entityMapNodeResolverQuery("Workload", "name", from, "id", 3, limit))
		return queries
	case "workload_instance":
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("WorkloadInstance", "id", from, "id", 0, limit),
			entityMapNodeResolverQuery("WorkloadInstance", "workload_id", from, "id", 1, limit),
			entityMapNodeResolverQuery("WorkloadInstance", "name", from, "id", 2, limit),
		}
	case "repository", "repo":
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("Repository", "id", from, "id", 0, limit),
			entityMapNodeResolverQuery("Repository", "name", from, "id", 1, limit),
			entityMapNodeResolverQuery("Repository", "path", from, "id", 2, limit),
		}
	case "resource", "cloud", "cloud_resource":
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("CloudResource", "id", from, "id", 0, limit),
			entityMapNodeResolverQuery("CloudResource", "resource_id", from, "id", 1, limit),
			entityMapNodeResolverQuery("CloudResource", "name", from, "id", 2, limit),
		}
	case "terraform", "tf", "terraform_resource":
		// #5443: TerraformResource (config-declared) and TerraformStateResource
		// (state-observed) are tried as distinct candidates, never a label
		// disjunction (MATCH (n:A|B) returns 0 rows on the pinned NornicDB
		// executor -- see docs/public/reference/nornicdb-pitfalls.md), so a
		// caller's address/uid/name resolves against whichever kind actually
		// has it.
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("TerraformResource", "address", from, "uid", 0, limit),
			entityMapNodeResolverQuery("TerraformResource", "uid", from, "uid", 1, limit),
			entityMapNodeResolverQuery("TerraformResource", "name", from, "uid", 2, limit),
			entityMapNodeResolverQuery("TerraformStateResource", "address", from, "uid", 3, limit),
			entityMapNodeResolverQuery("TerraformStateResource", "uid", from, "uid", 4, limit),
			entityMapNodeResolverQuery("TerraformStateResource", "name", from, "uid", 5, limit),
			entityMapNodeResolverQuery("TerraformDataSource", "address", from, "uid", 6, limit),
			entityMapNodeResolverQuery("TerraformDataSource", "uid", from, "uid", 7, limit),
		}
	case "terraform_datasource":
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("TerraformDataSource", "address", from, "uid", 0, limit),
			entityMapNodeResolverQuery("TerraformDataSource", "uid", from, "uid", 1, limit),
			entityMapNodeResolverQuery("TerraformDataSource", "name", from, "uid", 2, limit),
		}
	case "k8s", "kubernetes", "k8s_resource":
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("K8sResource", "id", from, "id", 0, limit),
			entityMapNodeResolverQuery("K8sResource", "qualified_name", from, "id", 1, limit),
			entityMapNodeResolverQuery("K8sResource", "name", from, "id", 2, limit),
		}
	case "terraform_module", "module":
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("TerraformModule", "uid", from, "uid", 0, limit),
			entityMapNodeResolverQuery("TerraformModule", "module_address", from, "uid", 1, limit),
			entityMapNodeResolverQuery("TerraformModule", "name", from, "uid", 2, limit),
		}
	case "file":
		return []entityMapResolverQuery{
			entityMapNodeResolverQuery("File", "path", from, "path", 0, limit),
			entityMapNodeResolverQuery("File", "relative_path", from, "path", 1, limit),
		}
	default:
		return entityMapGenericResolverQueries(from, limit)
	}
}

func entityMapGenericResolverQueries(from string, limit int) []entityMapResolverQuery {
	queries := []entityMapResolverQuery{entityMapNodeResolverQuery("Workload", "id", from, "id", 0, limit)}
	if canonicalID := canonicalWorkloadIDCandidate(from); canonicalID != from {
		queries = append(queries, entityMapNodeResolverQuery("Workload", "id", canonicalID, "id", 1, limit))
	}
	queries = append(
		queries,
		entityMapNodeResolverQuery("Workload", "name", from, "id", 2, limit),
		entityMapNodeResolverQuery("Repository", "id", from, "id", 3, limit),
		entityMapNodeResolverQuery("Repository", "name", from, "id", 4, limit),
		entityMapNodeResolverQuery("CloudResource", "id", from, "id", 5, limit),
		entityMapNodeResolverQuery("CloudResource", "resource_id", from, "id", 6, limit),
		entityMapNodeResolverQuery("TerraformResource", "address", from, "uid", 7, limit),
		entityMapNodeResolverQuery("TerraformResource", "uid", from, "uid", 8, limit),
		// #5443: TerraformStateResource is the state-observed sibling label;
		// tried as a distinct candidate, never a label disjunction (see the
		// terraform/tf case above for why).
		entityMapNodeResolverQuery("TerraformStateResource", "address", from, "uid", 9, limit),
		entityMapNodeResolverQuery("TerraformStateResource", "uid", from, "uid", 10, limit),
		entityMapNodeResolverQuery("TerraformDataSource", "address", from, "uid", 11, limit),
		entityMapNodeResolverQuery("K8sResource", "qualified_name", from, "id", 12, limit),
		entityMapNodeResolverQuery("File", "path", from, "path", 13, limit),
	)
	return queries
}

func entityMapNodeResolverQuery(
	label string,
	matchProperty string,
	from string,
	anchorProperty string,
	rank int,
	limit int,
) entityMapResolverQuery {
	return entityMapResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:%s {%s: $from})
WHERE ($environment = '' OR coalesce(n.environment, '') = '' OR n.environment = $environment)
  AND ($repo_id = '' OR coalesce(n.repo_id, n.id, '') = $repo_id)
RETURN coalesce(n.id, n.uid, n.resource_id, n.path, n.name) AS id,
       coalesce(n.name, n.address, n.qualified_name, n.path, n.id, n.uid) AS name,
       labels(n) AS labels,
       coalesce(n.repo_id, n.id, '') AS repo_id,
       coalesce(n.environment, '') AS environment,
       %q AS anchor_label,
       %q AS anchor_property,
       coalesce(n.%s, n.id, n.uid, n.path, n.name) AS anchor_value,
       %d AS rank
ORDER BY rank, name, id
LIMIT %d`, label, matchProperty, label, anchorProperty, anchorProperty, rank, limit),
		params: map[string]any{"from": from},
	}
}

func entityMapWorkloadRepoScopedResolverQuery(from string, repoID string, rank int, limit int) entityMapResolverQuery {
	return entityMapResolverQuery{
		cypher: fmt.Sprintf(`MATCH (n:Workload {repo_id: $repo_id, name: $from})
WHERE ($environment = '' OR coalesce(n.environment, '') = '' OR n.environment = $environment)
RETURN n.id AS id,
       n.name AS name,
       labels(n) AS labels,
       n.repo_id AS repo_id,
       coalesce(n.environment, '') AS environment,
       "Workload" AS anchor_label,
       "id" AS anchor_property,
       n.id AS anchor_value,
       %d AS rank
ORDER BY rank, name, id
LIMIT %d`, rank, limit),
		params: map[string]any{"from": from, "repo_id": repoID},
	}
}

func appendEntityMapCandidates(existing []entityMapCandidate, incoming []entityMapCandidate, limit int) []entityMapCandidate {
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

func entityMapCandidates(rows []map[string]any) []entityMapCandidate {
	candidates := make([]entityMapCandidate, 0, len(rows))
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
		candidates = append(candidates, entityMapCandidate{
			ID:             id,
			Name:           StringVal(row, "name"),
			Labels:         StringSliceVal(row, "labels"),
			RepoID:         StringVal(row, "repo_id"),
			Environment:    StringVal(row, "environment"),
			AnchorLabel:    StringVal(row, "anchor_label"),
			AnchorProperty: StringVal(row, "anchor_property"),
			AnchorValue:    StringVal(row, "anchor_value"),
		})
	}
	return candidates
}

func entityMapCandidateMaps(candidates []entityMapCandidate) []map[string]any {
	values := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Map())
	}
	return values
}

func (c entityMapCandidate) Map() map[string]any {
	value := map[string]any{"id": c.ID, "name": c.Name, "labels": c.Labels}
	if c.RepoID != "" {
		value["repo_id"] = c.RepoID
	}
	if c.Environment != "" {
		value["environment"] = c.Environment
	}
	if c.AnchorLabel != "" && c.AnchorProperty != "" && c.AnchorValue != "" {
		value["anchor"] = map[string]any{
			"label":    c.AnchorLabel,
			"property": c.AnchorProperty,
			"value":    c.AnchorValue,
		}
	}
	return value
}
