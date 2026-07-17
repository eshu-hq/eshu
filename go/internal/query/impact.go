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

	cypher := fmt.Sprintf(`MATCH (start:%s) WHERE start.id = $start_id
		OPTIONAL MATCH path = (start)-[rels*1..%d]->(repo:Repository)
		WITH start, path, repo, length(path) as depth, [rel IN relationships(path) | {type: type(rel), confidence: rel.confidence, reason: rel.reason}] as hops
		RETURN DISTINCT start.id as start_id, start.name as start_name, labels(start) as start_labels, repo.id as repo_id, repo.name as repo_name, depth, hops
		ORDER BY depth, repo_name, repo_id
		LIMIT $limit`, impactAnchorLabelDisjunction, req.MaxDepth)

	params := map[string]any{"start_id": req.Start, "limit": limit + 1}
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var start map[string]any
	if len(rows) > 0 {
		start = map[string]any{"id": StringVal(rows[0], "start_id"), "name": StringVal(rows[0], "start_name"), "labels": StringSliceVal(rows[0], "start_labels")}
	} else {
		startRow, err := h.Neo4j.RunSingle(r.Context(), "MATCH (n:"+impactAnchorLabelDisjunction+") WHERE n.id = $id RETURN n.id as id, n.name as name, labels(n) as labels", map[string]any{"id": req.Start})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		start = map[string]any{"id": StringVal(startRow, "id"), "name": StringVal(startRow, "name"), "labels": StringSliceVal(startRow, "labels")}
	}
	rows, truncated := trimImpactRows(rows, limit)
	paths := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		repoID := StringVal(row, "repo_id")
		if repoID == "" {
			continue
		}
		path := map[string]any{"repo_id": repoID, "repo_name": StringVal(row, "repo_name"), "depth": IntVal(row, "depth")}
		if hopsRaw := row["hops"]; hopsRaw != nil {
			if hopsSlice, ok := hopsRaw.([]any); ok {
				hops := make([]map[string]any, 0, len(hopsSlice))
				for _, hopRaw := range hopsSlice {
					if hopMap, ok := hopRaw.(map[string]any); ok {
						hop := map[string]any{"type": StringVal(hopMap, "type")}
						if conf, ok := hopMap["confidence"].(float64); ok {
							hop["confidence"] = conf
						}
						if reason := StringVal(hopMap, "reason"); reason != "" {
							hop["reason"] = reason
						}
						hops = append(hops, hop)
					}
				}
				path["hops"] = hops
			}
		}
		paths = append(paths, path)
	}
	resp := map[string]any{"start": start, "paths": paths, "count": len(paths), "limit": limit, "truncated": truncated}
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

	cypher := `MATCH (source:` + impactAnchorLabelDisjunction + `) WHERE source.id = $source_id
		MATCH (target:` + impactAnchorLabelDisjunction + `) WHERE target.id = $target_id
		OPTIONAL MATCH path = shortestPath((source)-[*1..8]-(target))
		WITH source, target, path, CASE WHEN path IS NOT NULL THEN [rel IN relationships(path) | {from_id: startNode(rel).id, from_name: startNode(rel).name, to_id: endNode(rel).id, to_name: endNode(rel).name, type: type(rel), confidence: rel.confidence, reason: rel.reason}] ELSE null END as hops
		RETURN source.id as source_id, source.name as source_name, labels(source) as source_labels, target.id as target_id, target.name as target_name, labels(target) as target_labels, CASE WHEN path IS NOT NULL THEN length(path) ELSE -1 END as depth, hops`

	params := map[string]any{
		"source_id": req.Source,
		"target_id": req.Target,
	}

	row, err := h.Neo4j.RunSingle(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if row == nil {
		WriteError(w, http.StatusNotFound, "source or target not found")
		return
	}

	source := map[string]any{
		"id":     StringVal(row, "source_id"),
		"name":   StringVal(row, "source_name"),
		"labels": StringSliceVal(row, "source_labels"),
	}

	target := map[string]any{
		"id":     StringVal(row, "target_id"),
		"name":   StringVal(row, "target_name"),
		"labels": StringSliceVal(row, "target_labels"),
	}

	depth := IntVal(row, "depth")
	var pathInfo map[string]any
	var overallConfidence float64
	var overallReason string

	if depth >= 0 {
		pathInfo = map[string]any{"depth": depth}

		// Extract hops
		if hopsRaw := row["hops"]; hopsRaw != nil {
			if hopsSlice, ok := hopsRaw.([]any); ok {
				hops := make([]map[string]any, 0, len(hopsSlice))
				confSum := 0.0
				confCount := 0
				reasons := []string{}

				for _, hopRaw := range hopsSlice {
					if hopMap, ok := hopRaw.(map[string]any); ok {
						hop := map[string]any{
							"from_id":   StringVal(hopMap, "from_id"),
							"from_name": StringVal(hopMap, "from_name"),
							"to_id":     StringVal(hopMap, "to_id"),
							"to_name":   StringVal(hopMap, "to_name"),
							"type":      StringVal(hopMap, "type"),
						}
						if conf, ok := hopMap["confidence"].(float64); ok {
							hop["confidence"] = conf
							confSum += conf
							confCount++
						}
						if reason := StringVal(hopMap, "reason"); reason != "" {
							hop["reason"] = reason
							reasons = append(reasons, reason)
						}
						hops = append(hops, hop)
					}
				}

				pathInfo["hops"] = hops

				// Calculate average confidence
				if confCount > 0 {
					overallConfidence = confSum / float64(confCount)
				}
				// Aggregate reasons
				if len(reasons) > 0 {
					overallReason = reasons[0]
					if len(reasons) > 1 {
						overallReason = fmt.Sprintf("%s (and %d more)", reasons[0], len(reasons)-1)
					}
				}
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
