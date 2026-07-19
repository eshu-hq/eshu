// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
)

// resourceInvestigationResourceAnchor renders the resource identity predicate
// used to anchor the workloads and repository-path reads on the resolved
// candidate. The prior form
// `coalesce(...) = $resource_id OR ($resource_arn <> ” AND resource.arn = $resource_arn)`
// returns zero rows on the pinned NornicDB build: the empty-string guard
// disjunct mis-evaluates so the whole predicate collapses (#5287). The arn
// disjunct is therefore appended only when the resolved candidate carries an
// arn, and the `<> ”` guard is dropped.
func resourceInvestigationResourceAnchor(alias string, hasArn bool) string {
	pred := fmt.Sprintf(
		"coalesce(%s.id, %s.uid, %s.resource_id, %s.name) = $resource_id",
		alias, alias, alias, alias,
	)
	if hasArn {
		pred += fmt.Sprintf(" OR %s.arn = $resource_arn", alias)
	}
	return pred
}

// resourceInvestigationAnchorParams binds the anchor params, including the arn
// param only when the resolved candidate carries one (kept in lockstep with
// resourceInvestigationResourceAnchor).
func resourceInvestigationAnchorParams(selected *resourceInvestigationCandidate, extra map[string]any) map[string]any {
	params := map[string]any{"resource_id": selected.ID}
	if selected.Arn != "" {
		params["resource_arn"] = selected.Arn
	}
	for k, v := range extra {
		params[k] = v
	}
	return params
}

// resourceInvestigationAnchorLabel returns a single infra label to fold into the
// traversal pattern for the resolved candidate, bounding the scan to that
// label's population instead of an unlabeled whole-graph start — the late-filter
// shape that produced repo-scale traversals (#5271/#5281, Codex review on #5302).
// Only a known infra label is returned, so the interpolated label is never
// attacker-influenced Cypher text. The section loader rejects an unknown or
// empty label before any graph read rather than issuing an unlabeled scan.
func resourceInvestigationAnchorLabel(selected *resourceInvestigationCandidate) string {
	for _, label := range selected.Labels {
		if infraLabelAllowed(label) {
			return label
		}
	}
	return ""
}

// resourceInvestigationResourceRef renders the `resource` pattern reference,
// folding in the resolved infra label when one is known so the traversal anchors
// on a bounded label population.
func resourceInvestigationResourceRef(selected *resourceInvestigationCandidate) string {
	if label := resourceInvestigationAnchorLabel(selected); label != "" {
		return "resource:" + label
	}
	return "resource"
}

func resourceInvestigationWorkloadsCypher(selected *resourceInvestigationCandidate) string {
	anchor := resourceInvestigationResourceAnchor("resource", selected.Arn != "")
	return fmt.Sprintf(`MATCH (instance:WorkloadInstance)-[rel:USES]->(%s)
WHERE (%s)
  AND ($environment = '' OR coalesce(instance.environment, resource.environment, '') = '' OR instance.environment = $environment)
RETURN DISTINCT instance.id AS instance_id,
       coalesce(instance.environment, resource.environment, '') AS environment,
       instance.workload_id AS workload_id_raw,
       instance.name AS instance_name,
       type(rel) AS relationship_type,
       coalesce(rel.reason, rel.evidence_type, '') AS relationship_reason,
       rel.confidence AS confidence
ORDER BY instance_id
LIMIT $limit`, resourceInvestigationResourceRef(selected), anchor)
}

func resourceInvestigationInstanceWorkloadsCypher() string {
	return `MATCH (instance:WorkloadInstance)-[:INSTANCE_OF]->(workload:Workload)
WHERE instance.id IN $instance_ids
RETURN instance.id AS instance_id, workload.id AS workload_id, workload.name AS workload_name, workload.repo_id AS workload_repo_id`
}

func resourceInvestigationRepoPathsCypher(
	req resourceInvestigationRequest,
	selected *resourceInvestigationCandidate,
	direction string,
) string {
	resourceRef := resourceInvestigationResourceRef(selected)
	pattern := fmt.Sprintf("(%s)-[rels*1..%d]->(repo:Repository)", resourceRef, req.MaxDepth)
	if direction == "incoming" {
		pattern = fmt.Sprintf("(%s)<-[rels*1..%d]-(repo:Repository)", resourceRef, req.MaxDepth)
	}
	anchor := resourceInvestigationResourceAnchor("resource", selected.Arn != "")
	return fmt.Sprintf(`MATCH path = %s
WHERE %s
RETURN DISTINCT repo.id AS repo_id,
       repo.name AS repo_name,
       length(path) AS depth,
       relationships(path) AS rels
ORDER BY depth, repo_name, repo_id
LIMIT $limit`, pattern, anchor)
}

// resourceInvestigationWorkloads returns the workload instances that USE the
// resolved resource. The prior single query chained
// MATCH + MATCH + OPTIONAL MATCH + WITH into a computed RETURN, which the pinned
// NornicDB build corrupts to all-null columns (#5287). It is split into two
// single-clause reads joined in Go: (1) the USES instances with relationship
// provenance and environment, and (2) the INSTANCE_OF workload identity for
// those instances.
func (h *ImpactHandler) resourceInvestigationWorkloads(
	ctx context.Context,
	req resourceInvestigationRequest,
	selected *resourceInvestigationCandidate,
	access repositoryAccessFilter,
) ([]map[string]any, bool, error) {
	instancesCypher := resourceInvestigationWorkloadsCypher(selected)
	rows, err := h.Neo4j.Run(ctx, instancesCypher, resourceInvestigationAnchorParams(selected, map[string]any{
		"environment": req.Environment,
		"limit":       req.Limit + 1,
	}))
	if err != nil {
		return nil, false, fmt.Errorf("load resource workloads: %w", err)
	}

	// #5167 W3 P1: resolve and grant-filter the FULL fetched row set BEFORE
	// trimming to the limit. Trimming first (the prior order) let a cross-tenant
	// workload sorted ahead of a granted one consume a limit slot and drop the
	// granted row that fell past the boundary, so a scoped caller saw fewer rows
	// than entitled and the truncated flag reflected the raw set, not the
	// granted set. The grant key (workload.repo_id) is only known after the
	// INSTANCE_OF resolution, so that resolution now runs over every fetched row.
	workloadByInstance, err := h.resourceInvestigationInstanceWorkloads(ctx, rows)
	if err != nil {
		return nil, false, err
	}

	workloads := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		instanceID := StringVal(row, "instance_id")
		workloadIDRaw := StringVal(row, "workload_id_raw")
		resolved := workloadByInstance[instanceID]
		// The resolved resource anchor is already grant-checked (its candidate was
		// filtered in resolveResourceInvestigationTarget), but a resource can be
		// USEd by a workload in a different repository, so each dependent workload
		// is bound to the grant independently here.
		if !impactRepoIDAllowed(resolved.repoID, access) {
			continue
		}
		workload := compactStringMap(map[string]any{
			"workload_id":         firstNonEmpty(resolved.id, workloadIDRaw, instanceID),
			"workload_name":       firstNonEmpty(resolved.name, StringVal(row, "instance_name"), workloadIDRaw),
			"instance_id":         instanceID,
			"environment":         StringVal(row, "environment"),
			"relationship_type":   StringVal(row, "relationship_type"),
			"relationship_reason": StringVal(row, "relationship_reason"),
		})
		if confidence := row["confidence"]; confidence != nil {
			workload["confidence"] = confidence
		}
		workloads = append(workloads, workload)
	}
	// Preserve the prior display order (workload_name, workload_id, instance_id);
	// the instance read orders by instance_id because the workload name is only
	// known after the second read.
	sort.SliceStable(workloads, func(i, j int) bool {
		return resourceInvestigationWorkloadSortKey(workloads[i]) < resourceInvestigationWorkloadSortKey(workloads[j])
	})
	// Trim the grant-filtered, sorted set so truncated reflects the rows the
	// caller may actually see.
	workloads, truncated := trimImpactRows(workloads, req.Limit)
	return workloads, truncated, nil
}

// resourceInvestigationWorkloadRef is a resolved INSTANCE_OF workload identity.
type resourceInvestigationWorkloadRef struct {
	id     string
	name   string
	repoID string
}

// resourceInvestigationInstanceWorkloads resolves the INSTANCE_OF workload for
// each instance id in rows via one single-clause read, returning a map keyed by
// instance id. An empty input yields no query.
func (h *ImpactHandler) resourceInvestigationInstanceWorkloads(
	ctx context.Context,
	rows []map[string]any,
) (map[string]resourceInvestigationWorkloadRef, error) {
	instanceIDs := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := StringVal(row, "instance_id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		instanceIDs = append(instanceIDs, id)
	}
	resolved := make(map[string]resourceInvestigationWorkloadRef, len(instanceIDs))
	if len(instanceIDs) == 0 {
		return resolved, nil
	}
	rows, err := h.Neo4j.Run(ctx, resourceInvestigationInstanceWorkloadsCypher(),
		map[string]any{"instance_ids": instanceIDs})
	if err != nil {
		return nil, fmt.Errorf("resolve instance workloads: %w", err)
	}
	for _, row := range rows {
		resolved[StringVal(row, "instance_id")] = resourceInvestigationWorkloadRef{
			id:     StringVal(row, "workload_id"),
			name:   StringVal(row, "workload_name"),
			repoID: StringVal(row, "workload_repo_id"),
		}
	}
	return resolved, nil
}

// resourceInvestigationWorkloadSortKey mirrors the prior
// `ORDER BY workload_name, workload_id, instance_id` display order.
func resourceInvestigationWorkloadSortKey(workload map[string]any) string {
	return StringVal(workload, "workload_name") + "\x00" +
		StringVal(workload, "workload_id") + "\x00" +
		StringVal(workload, "instance_id")
}

// resourceInvestigationRepoPaths returns repository paths reachable from the
// resolved resource. The prior query chained a resource-anchor MATCH with the
// path MATCH and projected a map-valued `[rel IN relationships(path) | {…}]`
// comprehension, both of which the pinned NornicDB build corrupts (#5287). It is
// rewritten to a single-clause path read that projects raw relationships(path);
// the per-hop {type, confidence, reason} maps are rebuilt in Go, which preserves
// full hop provenance (the raw edge properties survive where the comprehension
// does not).
func (h *ImpactHandler) resourceInvestigationRepoPaths(
	ctx context.Context,
	req resourceInvestigationRequest,
	selected *resourceInvestigationCandidate,
	direction string,
	access repositoryAccessFilter,
) ([]map[string]any, bool, error) {
	cypher := resourceInvestigationRepoPathsCypher(req, selected, direction)
	rows, err := h.Neo4j.Run(ctx, cypher, resourceInvestigationAnchorParams(selected, map[string]any{
		"limit": req.Limit + 1,
	}))
	if err != nil {
		return nil, false, fmt.Errorf("load resource repository paths: %w", err)
	}
	paths := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, map[string]any{
			"repo_id":   StringVal(row, "repo_id"),
			"repo_name": StringVal(row, "repo_name"),
			"direction": direction,
			"depth":     IntVal(row, "depth"),
			"hops":      resourceInvestigationHopList(row["rels"]),
		})
	}
	// #5167 W3 P1: each path terminates at a Repository node; bind every one to
	// the caller's grant so a resource that traces to a cross-tenant repo does
	// not surface that repo's id/name. The grant filter runs on the FULL fetched
	// set BEFORE the trim so a cross-tenant path sorted ahead of a granted one
	// cannot consume a limit slot and drop the granted path past the boundary;
	// truncated then reflects the granted set the caller may actually see.
	paths, truncated := trimImpactRows(filterRowsByRepoIDForAccess(paths, access), req.Limit)
	return paths, truncated, nil
}
