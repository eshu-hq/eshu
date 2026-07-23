// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	// changeSurfaceLegacyDefaultDepth bounds the variable-length impact
	// traversal when the caller does not request a depth. It mirrors the
	// investigation handler default so both change-surface surfaces stay
	// within the same repo-scale traversal budget.
	changeSurfaceLegacyDefaultDepth = 4
	// changeSurfaceLegacyMaxDepth caps the requested traversal depth. The prior
	// hardcoded *1..8 expansion over a densely connected Workload node exploded
	// the path frontier at repo scale (issue #3384); clamping keeps the worst
	// case bounded while preserving the documented 8-hop reach.
	changeSurfaceLegacyMaxDepth = 8
)

// findChangeSurface analyzes the legacy entity-anchored change surface.
// Prefer investigateChangeSurface for prompt-facing code-topic and path flows.
//
// The start node is resolved through label-anchored, indexed probes (driven by
// the optional kind/target_type hint, with an ordered label fallback when it is
// absent) instead of an unlabeled `MATCH (start) WHERE start.id = $target_id`
// scan, and the impact traversal anchors the resolved label with a bounded,
// parameterized depth. Both changes remove the full-node scan and the unbounded
// 8-hop expansion that hung service-kind targets at repo scale (issue #3384).
func (h *ImpactHandler) findChangeSurface(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.change_surface") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"change surface analysis requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.change_surface",
			h.profile(),
			requiredProfile("platform_impact.change_surface"),
		)
		return
	}

	var req struct {
		Target      string `json:"target"`
		Kind        string `json:"kind"`
		TargetType  string `json:"target_type"`
		Environment string `json:"environment"`
		MaxDepth    int    `json:"max_depth"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Target == "" {
		WriteError(w, http.StatusBadRequest, "target is required")
		return
	}

	limit := normalizeImpactListLimit(req.Limit)
	depth := normalizeChangeSurfaceLegacyDepth(req.MaxDepth)

	// Resolve the start node with bounded, label-anchored indexed probes. The
	// kind/target_type hint selects the probe set; an empty hint falls back to
	// the ordered label probes. Neither path issues an unlabeled scan.
	resolverReq := changeSurfaceInvestigationRequest{
		Target:     req.Target,
		TargetType: legacyChangeSurfaceTargetType(req.Kind, req.TargetType),
		Limit:      limit,
	}
	// resolveChangeSurfaceTarget already binds the resolved start candidate to
	// the caller's grant (#5167 W3); findChangeSurfaceImpactRows independently
	// binds every impacted row below, since the traversal can transitively
	// reach a repository the caller does not hold.
	selected, _, err := h.resolveChangeSurfaceTarget(r.Context(), resolverReq)
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.change_surface") {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	target := map[string]any{"id": req.Target}
	impacted := make([]map[string]any, 0)
	truncated := false
	if selected != nil {
		target = map[string]any{"id": selected.ID, "name": selected.Name}
		rows, rowsTruncated, traversalErr := h.findChangeSurfaceImpactRows(r.Context(), *selected, req.Environment, depth, limit, repositoryAccessFilterFromContext(r.Context()))
		if traversalErr != nil {
			if WriteGraphReadError(w, r, traversalErr, "platform_impact.change_surface") {
				return
			}
			WriteError(w, http.StatusInternalServerError, traversalErr.Error())
			return
		}
		impacted = rows
		truncated = rowsTruncated
	}

	resp := map[string]any{"target": target, "impacted": impacted, "count": len(impacted), "limit": limit, "truncated": truncated}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.change_surface", TruthBasisHybrid, "resolved from graph and impact relationships"))
}

// changeSurfaceLegacyCypher is the single-anchor-clause impact traversal for the
// legacy endpoint. Verbs: %s = the label-anchored start node pattern, %d = max
// traversal depth, %s = the optional environment predicate (empty or an
// ` AND (…)` clause). It projects the raw relationships(path) list (unwound
// per-edge in Go) rather than the old OPTIONAL MATCH + UNWIND + WITH + RETURN
// DISTINCT shape, which returned a single all-null row on the pinned NornicDB
// (#5287). The environment filter is applied server-side (before LIMIT) so an
// environment-scoped read cannot under-report when the limit is reached.
const changeSurfaceLegacyCypher = `MATCH path = %s-[*1..%d]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN ['Repository', 'Workload', 'WorkloadInstance', 'CloudResource', 'TerraformModule', 'DataAsset'])%s
RETURN impacted.id as id, impacted.name as name, labels(impacted) as labels, impacted.environment as environment,
	impacted.repo_id as repo_id, length(path) as depth, relationships(path) as rels
ORDER BY depth, name, id
LIMIT $limit`

// findChangeSurfaceImpactRows runs the bounded impact traversal from a resolved
// start node. It anchors the resolved label in the start MATCH (indexed lookup),
// caps the variable-length expansion at depth, over-fetches one row beyond limit
// to detect truncation honestly, and preserves the legacy per-relationship
// projection (rel_type, confidence, reason) so callers keep edge provenance.
func (h *ImpactHandler) findChangeSurfaceImpactRows(
	ctx context.Context,
	target changeSurfaceTargetCandidate,
	environment string,
	depth int,
	limit int,
	access repositoryAccessFilter,
) ([]map[string]any, bool, error) {
	// #5167 W3: mirror changeSurfaceImpactRows's empty-grant short circuit --
	// the caller's target is already resolved through the grant-filtered
	// resolveChangeSurfaceTarget, but a scoped caller with no granted
	// repositories must never see impacted rows.
	if access.empty() {
		return nil, false, nil
	}
	startPattern, err := changeSurfaceTraversalStartPattern(target)
	if err != nil {
		return nil, false, err
	}
	// Single anchoring clause: the pinned NornicDB build mis-executes the old
	// OPTIONAL MATCH + UNWIND relationships(path) + WITH + RETURN DISTINCT shape
	// (it returned a single all-null row — #5287, proven live). Fold the start
	// anchor into the path pattern, project the raw relationships(path) list, and
	// unwind the per-edge provenance in Go. Two constraints from the pinned build:
	// a `[rel IN relationships(path) | rel.confidence]` comprehension is NOT safe
	// (it stringifies the edge map), and the old `$environment = '' OR
	// coalesce(…, '') = ''` predicate silently drops every row when combined with
	// the relationships(path) projection — so the environment filter uses the
	// narrower NornicDB-safe form in changeSurfaceEnvironmentClause, applied
	// server-side before LIMIT and re-checked in Go below.
	cypher := fmt.Sprintf(changeSurfaceLegacyCypher, startPattern, depth, changeSurfaceEnvironmentClause(environment))
	params := map[string]any{
		"target_id": target.ID,
		"limit":     limit + 1,
	}
	if environment != "" {
		params["environment"] = environment
	}
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, false, err
	}

	// Unwind relationships(path) per impacted path into per-edge provenance
	// entries (the old UNWIND relationships(path) done in Go), deduplicating on
	// the same tuple the old RETURN DISTINCT collapsed.
	entries := make([]map[string]any, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		env := StringVal(row, "environment")
		if environment != "" && env != "" && env != environment {
			continue
		}
		if !impactRepoIDAllowed(changeSurfaceImpactedRowRepoID(row), access) {
			continue
		}
		base := map[string]any{"id": StringVal(row, "id"), "name": StringVal(row, "name"), "labels": StringSliceVal(row, "labels"), "depth": IntVal(row, "depth")}
		if env != "" {
			base["environment"] = env
		}
		for _, edge := range changeSurfaceRelEdges(row["rels"]) {
			entry := map[string]any{"id": base["id"], "name": base["name"], "labels": base["labels"], "depth": base["depth"]}
			if env != "" {
				entry["environment"] = env
			}
			if edge.relType != "" {
				entry["rel_type"] = edge.relType
			}
			if edge.hasConfidence {
				entry["confidence"] = edge.confidence
			}
			if edge.reason != "" {
				entry["reason"] = edge.reason
			}
			// Dedup on the same tuple the old RETURN DISTINCT collapsed. Encode
			// confidence with a sentinel for the unset case so a missing confidence
			// and an explicit 0.0 do not produce different keys (a `%v` of a nil map
			// value formats as "<nil>" but 0.0 formats as "0").
			confKey := "\x00unset"
			if edge.hasConfidence {
				confKey = fmt.Sprintf("%g", edge.confidence)
			}
			key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", StringVal(entry, "id"), edge.relType, confKey, edge.reason, IntVal(entry, "depth"))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			entries = append(entries, entry)
		}
	}
	entries, truncated := trimImpactRows(entries, limit)
	return entries, truncated, nil
}

// changeSurfaceRelEdge is one relationship's provenance unwound from a graph
// path row.
type changeSurfaceRelEdge struct {
	relType       string
	confidence    float64
	hasConfidence bool
	reason        string
}

// changeSurfaceRelEdges extracts per-relationship provenance from a
// relationships(path) value. A scalar comprehension over rel properties corrupts
// on the pinned NornicDB, so the raw list is unwound here. The two supported
// backends serialize the list differently: the Neo4j Go driver returns
// `neo4j.Relationship` values (with Type/Props), while NornicDB returns each
// relationship as a `map[string]any` with a `type` and a nested `properties`
// map. Both shapes are decoded so the edge provenance survives on either backend.
func changeSurfaceRelEdges(raw any) []changeSurfaceRelEdge {
	rels, ok := raw.([]any)
	if !ok {
		return nil
	}
	edges := make([]changeSurfaceRelEdge, 0, len(rels))
	for _, item := range rels {
		switch rel := item.(type) {
		case neo4jdriver.Relationship:
			edges = append(edges, changeSurfaceRelEdgeFromProps(rel.Type, rel.Props))
		case map[string]any:
			props, _ := rel["properties"].(map[string]any)
			edges = append(edges, changeSurfaceRelEdgeFromProps(StringVal(rel, "type"), props))
		}
	}
	return edges
}

// changeSurfaceRelEdgeFromProps builds a provenance edge from a relationship type
// and its property map, tolerating a nil property map.
func changeSurfaceRelEdgeFromProps(relType string, props map[string]any) changeSurfaceRelEdge {
	edge := changeSurfaceRelEdge{relType: relType}
	if conf, ok := props["confidence"].(float64); ok {
		edge.confidence = conf
		edge.hasConfidence = true
	}
	if reason, ok := props["reason"].(string); ok {
		edge.reason = reason
	}
	return edge
}

// legacyChangeSurfaceTargetType maps the legacy request hint to a normalized
// resolver target type. It accepts both the historical `target_type` field and
// the issue #3384 `kind` field, preferring kind when both are present, and
// normalizes the value so it routes to the label-anchored resolver probes. An
// unrecognized hint normalizes to empty, which uses the ordered label fallback.
func legacyChangeSurfaceTargetType(kind, targetType string) string {
	if kind != "" {
		return normalizeChangeSurfaceTargetType(kind)
	}
	return normalizeChangeSurfaceTargetType(targetType)
}

// normalizeChangeSurfaceLegacyDepth defaults and clamps the requested traversal
// depth to the bounded range so a caller cannot reintroduce the deep scan.
func normalizeChangeSurfaceLegacyDepth(depth int) int {
	if depth <= 0 {
		return changeSurfaceLegacyDefaultDepth
	}
	if depth > changeSurfaceLegacyMaxDepth {
		return changeSurfaceLegacyMaxDepth
	}
	return depth
}
