// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ImpactHandler serves HTTP endpoints for impact analysis queries including
// blast radius, change surface, resource-to-code tracing, and dependency paths.
type ImpactHandler struct {
	Neo4j   GraphQuery
	Content ContentStore
	Profile QueryProfile
	Logger  *slog.Logger
	// Instruments backs operator-facing metrics for degraded-but-successful
	// impact reads, e.g. QueryK8sSelectCandidateScanTruncated when a
	// deployment-trace directed SELECTS candidate scan is truncated at the
	// repository entity limit (#5363). Nil is tolerated (metric emission is
	// skipped) so tests can construct ImpactHandler without wiring the full
	// telemetry stack.
	Instruments *telemetry.Instruments
	// KubernetesPodTemplates is the Postgres-backed identity-bound
	// kubernetes_live.pod_template read model. When non-nil,
	// trace_deployment_chain probes it (via fetchWorkloadLiveEvidence) for a
	// live pod matching the traced workload's OWN declared ArgoCD identity
	// (argocd.argoproj.io/tracking-id) to promote the deployment truth tier
	// from config_only to runtime_confirmed (#5471 codex P1). Nil is
	// tolerated (tests, unwired profiles) and degrades gracefully to
	// config-only classification.
	//
	// This replaced an earlier KubernetesCorrelations-backed probe
	// (PostgresKubernetesCorrelationStore) that promoted on an
	// image-digest-only match with no binding to the traced workload's own
	// identity -- two workloads sharing a base image digest could promote
	// one workload on another's live row. KubernetesPodTemplates fixes that
	// by requiring an identity match first.
	KubernetesPodTemplates KubernetesPodTemplateStore
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

	// Resolve the start node's label and canonical id from the caller identifier
	// (id or name). The pinned NornicDB build matches zero rows for a
	// `MATCH (n:A|B|C) WHERE n.id = $id` label-disjunction anchor (#5286), so the
	// disjunction cannot seed the traversal directly.
	start, err := resolveImpactAnchorNode(r.Context(), h.Neo4j, "start_id", req.Start)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	startInfo := map[string]any{"id": req.Start}
	paths := make([]map[string]any, 0)
	truncated := false
	if start != nil {
		startInfo = map[string]any{"id": start.id, "name": start.name, "labels": start.labels}
		// Anchor the resolved label inline on the canonical id (indexed) and
		// project the raw relationships(path) list, unwound into per-hop
		// provenance in Go — the map-valued `[rel IN relationships(path) | {…}]`
		// comprehension is mangled on the pinned build.
		cypher := fmt.Sprintf(impactRepoPathCypher, start.pattern("start", "start_id"), req.MaxDepth)
		rows, rerr := h.Neo4j.Run(r.Context(), cypher, map[string]any{"start_id": start.id, "limit": limit + 1})
		if rerr != nil {
			WriteError(w, http.StatusInternalServerError, rerr.Error())
			return
		}
		var trimmed []map[string]any
		trimmed, truncated = trimImpactRows(rows, limit)
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
	// Anchor on the resolved canonical ids (the caller may have passed a name).
	row, err := h.Neo4j.RunSingle(r.Context(), cypher, map[string]any{"source_id": sourceNode.id, "target_id": targetNode.id})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var pathInfo map[string]any
	var overallConfidence float64
	var overallReason string

	// A path exists only when nodes(path) is non-empty. Guarding on the node list
	// rather than `row != nil` keeps the "no path" case correct on a backend where
	// shortestPath returns a single null-valued record (nodes(path) IS NULL)
	// instead of zero rows — otherwise the handler would report a bogus
	// `path: {depth: 0, hops: []}`.
	if pathNodes := impactNodeIdentityList(row["ns"]); len(pathNodes) > 0 {
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
