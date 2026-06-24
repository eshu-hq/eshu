// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
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
	selected, _, err := h.resolveChangeSurfaceTarget(r.Context(), resolverReq)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	target := map[string]any{"id": req.Target}
	impacted := make([]map[string]any, 0)
	truncated := false
	if selected != nil {
		target = map[string]any{"id": selected.ID, "name": selected.Name}
		rows, rowsTruncated, traversalErr := h.findChangeSurfaceImpactRows(r.Context(), *selected, req.Environment, depth, limit)
		if traversalErr != nil {
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
) ([]map[string]any, bool, error) {
	startMatch, err := changeSurfaceTraversalStartMatch(target)
	if err != nil {
		return nil, false, err
	}
	cypher := fmt.Sprintf(`%s
OPTIONAL MATCH path = (start)-[rels*1..%d]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN ['Repository', 'Workload', 'WorkloadInstance', 'CloudResource', 'TerraformModule', 'DataAsset'])
	AND ($environment = '' OR coalesce(impacted.environment, '') = '' OR impacted.environment = $environment)
UNWIND relationships(path) as rel
WITH impacted, rel, length(path) as depth
RETURN DISTINCT impacted.id as id, impacted.name as name, labels(impacted) as labels, impacted.environment as environment,
	type(rel) as rel_type, rel.confidence as confidence, rel.reason as reason, depth
ORDER BY depth, impacted.name, impacted.id
LIMIT $limit`, startMatch, depth)

	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{
		"target_id":   target.ID,
		"environment": environment,
		"limit":       limit + 1,
	})
	if err != nil {
		return nil, false, err
	}

	rows, truncated := trimImpactRows(rows, limit)
	impacted := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		env := StringVal(row, "environment")
		if environment != "" && env != "" && env != environment {
			continue
		}
		entry := map[string]any{"id": StringVal(row, "id"), "name": StringVal(row, "name"), "labels": StringSliceVal(row, "labels"), "depth": IntVal(row, "depth")}
		if env != "" {
			entry["environment"] = env
		}
		if conf, ok := row["confidence"].(float64); ok {
			entry["confidence"] = conf
		}
		if reason := StringVal(row, "reason"); reason != "" {
			entry["reason"] = reason
		}
		impacted = append(impacted, entry)
	}
	return impacted, truncated, nil
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
