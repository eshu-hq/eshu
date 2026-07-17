// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"log/slog"
	"net/http"
)

// ImpactHandler serves HTTP endpoints for impact analysis queries including
// blast radius, change surface, resource-to-code tracing, and dependency paths.
type ImpactHandler struct {
	Neo4j   GraphQuery
	Content ContentStore
	Profile QueryProfile
	Logger  *slog.Logger
}

// Mount registers impact analysis routes on the given mux.
func (h *ImpactHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/impact/trace-deployment-chain", h.traceDeploymentChain)
	mux.HandleFunc("POST /api/v0/impact/deployment-config-influence", h.investigateDeploymentConfigInfluence)
	mux.HandleFunc("POST /api/v0/impact/blast-radius", h.findBlastRadius)
	mux.HandleFunc("POST /api/v0/impact/change-surface", h.findChangeSurface)
	mux.HandleFunc("POST /api/v0/impact/change-surface/investigate", h.investigateChangeSurface)
	mux.HandleFunc("POST /api/v0/impact/pre-change", h.preChangeImpact)
	mux.HandleFunc("POST /api/v0/impact/developer-change-plan", h.developerChangePlan)
	mux.HandleFunc("POST /api/v0/impact/contracts", h.contractImpact)
	mux.HandleFunc("POST /api/v0/impact/entity-map", h.entityMap)
	mux.HandleFunc("POST /api/v0/impact/resource-investigation", h.investigateResource)
	mux.HandleFunc("POST /api/v0/impact/trace-resource-to-code", h.traceResourceToCode)
	mux.HandleFunc("POST /api/v0/impact/explain-dependency-path", h.explainDependencyPath)
	mux.HandleFunc("POST /api/v0/impact/trace-exposure-path", h.traceExposurePath)
}

func (h *ImpactHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// traceResourceToCode traces a resource back to its code repository.
// POST /api/v0/impact/trace-resource-to-code
// Body: {"start": "entity-id", "environment": "production", "max_depth": 8}
func (h *ImpactHandler) traceResourceToCode(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.resource_to_code") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"resource-to-code tracing requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.resource_to_code",
			h.profile(),
			requiredProfile("platform_impact.resource_to_code"),
		)
		return
	}

	var req struct {
		Start       string `json:"start"`
		Environment string `json:"environment"`
		MaxDepth    int    `json:"max_depth"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Start == "" {
		WriteError(w, http.StatusBadRequest, "start is required")
		return
	}

	// Default and clamp max_depth
	if req.MaxDepth <= 0 {
		req.MaxDepth = 8
	}
	if req.MaxDepth > 20 {
		req.MaxDepth = 20
	}
	if req.MaxDepth < 1 {
		req.MaxDepth = 1
	}
	limit := normalizeImpactListLimit(req.Limit)

	// Folded per-label traversal: one CALL{UNION} query anchors each candidate
	// label inline and traverses to Repository, keeping label resolution and the
	// traversal in a single round-trip. The pinned NornicDB build matches zero
	// rows for a `MATCH (n:A|B|C) WHERE n.id = $id` label-disjunction anchor and
	// mangles the map-valued `[rel IN relationships(path) | {…}]` comprehension
	// (#5286), so each branch is a single-label inline-property anchor projecting
	// the raw relationships(path) list, unwound into per-hop provenance in Go.
	rows, err := h.Neo4j.Run(r.Context(), impactRepoTraversalCypher(req.MaxDepth), map[string]any{"start_id": req.Start, "limit": limit + 1})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	trimmed, truncated := trimImpactRows(rows, limit)
	startInfo := map[string]any{"id": req.Start}
	if len(rows) > 0 {
		startInfo["name"] = StringVal(rows[0], "start_name")
		startInfo["labels"] = StringSliceVal(rows[0], "start_labels")
	} else if start, rerr := resolveImpactAnchorNode(r.Context(), h.Neo4j, "start_id", req.Start); rerr != nil {
		// No Repository paths: hydrate the start node by id so the response still
		// identifies it.
		WriteError(w, http.StatusInternalServerError, rerr.Error())
		return
	} else if start != nil {
		startInfo = map[string]any{"id": start.id, "name": start.name, "labels": start.labels}
	}
	paths := make([]map[string]any, 0, len(trimmed))
	for _, row := range trimmed {
		repoID := StringVal(row, "repo_id")
		if repoID == "" {
			continue
		}
		paths = append(paths, map[string]any{
			"repo_id":   repoID,
			"repo_name": StringVal(row, "repo_name"),
			"depth":     IntVal(row, "depth"),
			"hops":      impactTraceHops(row["rels"]),
		})
	}
	resp := map[string]any{"start": startInfo, "paths": paths, "count": len(paths), "limit": limit, "truncated": truncated}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.resource_to_code", TruthBasisHybrid, "resolved from resource-to-code graph traversal"))
}

// explainDependencyPath finds and explains the shortest path between two entities.
// POST /api/v0/impact/explain-dependency-path
// Body: {"source": "entity-id", "target": "entity-id", "environment": "production"}
func (h *ImpactHandler) explainDependencyPath(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.dependency_path") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dependency path analysis requires full dependency graph truth",
			"unsupported_capability",
			"platform_impact.dependency_path",
			h.profile(),
			requiredProfile("platform_impact.dependency_path"),
		)
		return
	}

	var req struct {
		Source      string `json:"source"`
		Target      string `json:"target"`
		Environment string `json:"environment"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Source == "" {
		WriteError(w, http.StatusBadRequest, "source is required")
		return
	}
	if req.Target == "" {
		WriteError(w, http.StatusBadRequest, "target is required")
		return
	}

	// Resolve the source and target labels with per-label inline-property anchors
	// (one CALL{UNION} each); a `MATCH (n:A|B|C) WHERE n.id = $id` label-
	// disjunction anchor matches zero rows on the pinned NornicDB build (#5286).
	sourceNode, err := resolveImpactAnchorNode(r.Context(), h.Neo4j, "source_id", req.Source)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	targetNode, err := resolveImpactAnchorNode(r.Context(), h.Neo4j, "target_id", req.Target)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sourceNode == nil || targetNode == nil {
		WriteError(w, http.StatusNotFound, "source or target not found")
		return
	}

	source := map[string]any{"id": sourceNode.id, "name": sourceNode.name, "labels": sourceNode.labels}
	target := map[string]any{"id": targetNode.id, "name": targetNode.name, "labels": targetNode.labels}

	// Single anchoring clause: shortestPath with single-label inline-property
	// anchors on both ends, projecting the raw nodes(path)/relationships(path)
	// lists (zipped into hops in Go). The pinned build corrupts the old
	// two-disjunction-MATCH + WITH + map-valued rel comprehension shape.
	cypher := fmt.Sprintf(`MATCH path = shortestPath(%s-[*1..8]-%s)
RETURN length(path) AS depth, nodes(path) AS ns, relationships(path) AS rels`,
		sourceNode.pattern("source", "source_id"), targetNode.pattern("target", "target_id"))
	row, err := h.Neo4j.RunSingle(r.Context(), cypher, map[string]any{"source_id": req.Source, "target_id": req.Target})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var pathInfo map[string]any
	var overallConfidence float64
	var overallReason string

	if row != nil {
		depth := IntVal(row, "depth")
		pathInfo = map[string]any{"depth": depth}
		hops := impactDependencyHops(row["ns"], row["rels"])
		pathInfo["hops"] = hops

		confSum := 0.0
		confCount := 0
		reasons := []string{}
		for _, hop := range hops {
			if conf, ok := hop["confidence"].(float64); ok {
				confSum += conf
				confCount++
			}
			if reason := StringVal(hop, "reason"); reason != "" {
				reasons = append(reasons, reason)
			}
		}
		if confCount > 0 {
			overallConfidence = confSum / float64(confCount)
		}
		if len(reasons) > 0 {
			overallReason = reasons[0]
			if len(reasons) > 1 {
				overallReason = fmt.Sprintf("%s (and %d more)", reasons[0], len(reasons)-1)
			}
		}
	}

	resp := map[string]any{
		"source": source,
		"target": target,
	}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	if pathInfo != nil {
		resp["path"] = pathInfo
	}
	if overallConfidence > 0 {
		resp["confidence"] = overallConfidence
	}
	if overallReason != "" {
		resp["reason"] = overallReason
	}

	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(h.profile(), "platform_impact.dependency_path", TruthBasisHybrid, "resolved from shortest-path dependency traversal"))
}
