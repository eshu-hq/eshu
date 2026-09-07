// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ImpactHandler serves HTTP endpoints for impact analysis queries including
// blast radius, change surface, resource-to-code tracing, and dependency paths.
type ImpactHandler struct {
	Neo4j   querycontract.GraphQuery
	Content querycontract.ContentStore
	Profile querycontract.QueryProfile
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
	KubernetesPodTemplates impacttrace.KubernetesPodTemplateStore
	// CodeSurface runs the code-topic-coupled half of change-surface
	// resolution (fetch topic rows, bind grants, shape files/symbols/
	// evidence groups). It is an interface, not a method set, because the
	// implementation needs lane-A codeTopic* types that cannot cross
	// into this package (#6060 lane B). Wiring injects the root adapter;
	// tests inject fakes. B2-FOLLOWUP: collapse back into ImpactHandler
	// methods when lane A exports codeTopic*.
	CodeSurface ChangeSurfaceCodeBackend
	// TraceContext runs deployment-trace context enrichment, which builds
	// a B5-entity handler internally. Same interface-indirection rationale
	// as CodeSurface: entity construction cannot cross into this package.
	TraceContext DeploymentTraceContextProvider
	// PathProbe decodes raw graph path projections (nodes(path) and
	// relationships(path) values, which embed driver structs) into anchors
	// and hop provenance. It is an interface because the decoding switches
	// on the Neo4j driver types, and only the query root may import the
	// driver (package AGENTS.md, depguard query-no-graph-driver). Unlike
	// CodeSurface and TraceContext this indirection is permanent, not a
	// B2-FOLLOWUP: the driver seam stays in root by rule. Wiring injects
	// the root adapter; tests inject fakes.
	PathProbe ImpactPathProbeBackend
}

// Default backends used when a handler leaves the interface fields above
// unset. Package query assigns the production adapters in an init function,
// so the cmd wirings (which construct ImpactHandler with only the store
// fields) keep working unchanged; tests in this package inject fakes
// per-handler and never touch these globals.
var (
	// DefaultCodeSurface backs ImpactHandler.CodeSurface when unset.
	DefaultCodeSurface ChangeSurfaceCodeBackend
	// DefaultTraceContext backs ImpactHandler.TraceContext when unset.
	DefaultTraceContext DeploymentTraceContextProvider
	// DefaultPathProbe backs ImpactHandler.PathProbe when unset.
	DefaultPathProbe ImpactPathProbeBackend
)

func (h *ImpactHandler) codeSurface() ChangeSurfaceCodeBackend {
	if h != nil && h.CodeSurface != nil {
		return h.CodeSurface
	}
	return DefaultCodeSurface
}

func (h *ImpactHandler) traceContext() DeploymentTraceContextProvider {
	if h != nil && h.TraceContext != nil {
		return h.TraceContext
	}
	return DefaultTraceContext
}

func (h *ImpactHandler) pathProbe() ImpactPathProbeBackend {
	if h != nil && h.PathProbe != nil {
		return h.PathProbe
	}
	return DefaultPathProbe
}

// ChangeSurfaceCodeBackend fetches and shapes change-surface code evidence
// (topic rows plus changed-path symbols) for one investigation request.
// fetchPathSymbols is the caller's bound changeSurfacePathSymbols method:
// the adapter calls it under the same changed-paths guard the method form
// used, so behavior is identical with the lane-A-coupled fetch living in
// root. See #6060.
type ChangeSurfaceCodeBackend interface {
	FetchCodeSurface(
		ctx context.Context,
		h *ImpactHandler,
		req ChangeSurfaceInvestigationRequest,
		fetchPathSymbols func(context.Context, ChangeSurfaceInvestigationRequest) ([]map[string]any, bool, error),
	) (map[string]any, error)
}

// DeploymentTraceContextProvider enriches a deployment-trace request with
// service workload context. The production implementation builds a
// B5-entity handler, which cannot be named from this package. See #6060.
type DeploymentTraceContextProvider interface {
	FetchServiceTraceContext(
		ctx context.Context,
		graph querycontract.GraphQuery,
		content querycontract.ContentStore,
		logger *slog.Logger,
		serviceName string,
		traceOptions TraceEnrichmentConfig,
	) (map[string]any, error)
	// BuildServiceDeploymentOverview shapes the service deployment overview
	// for a workload context. It lives on this interface (rather than as a
	// shared helper) because the shaping reuses the service-story build
	// context, which cannot cross into this package either.
	BuildServiceDeploymentOverview(workloadContext map[string]any) map[string]any
}

// ImpactPathProbeBackend decodes raw graph path projections for the by-id
// impact reads (trace-resource-to-code, explain-dependency-path, and the
// resource-investigation hop reader). nodes(path) and relationships(path)
// values embed Neo4j driver structs, and only the query root may import the
// driver, so the production implementation lives in root and this package
// reaches it through the interface. See the PathProbe field.
type ImpactPathProbeBackend interface {
	// ResolveAnchor resolves a by-id node to its canonical label.
	ResolveAnchor(
		ctx context.Context,
		reader querycontract.GraphQuery,
		idParam, id string,
	) (*impacttrace.ResolvedImpactAnchor, error)
	// TraceHops builds trace-resource-to-code hop provenance from a raw
	// relationships(path) value.
	TraceHops(relsRaw any) []map[string]any
	// DependencyHops builds explain-dependency-path hop provenance from raw
	// nodes(path) and relationships(path) values.
	DependencyHops(nodesRaw, relsRaw any) []map[string]any
	// PathHasNodes reports whether a raw nodes(path) value holds any node.
	PathHasNodes(nodesRaw any) bool
	// ResourceInvestigationHops builds resource-investigation hop maps from
	// a raw relationships(path) value.
	ResourceInvestigationHops(relsRaw any) []map[string]any
}

// Mount registers impact analysis routes on the given mux.
func (h *ImpactHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/impact/trace-deployment-chain", h.TraceDeploymentChain)
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

func (h *ImpactHandler) profile() querycontract.QueryProfile {
	if h == nil {
		return querycontract.ProfileProduction
	}
	return querycontract.NormalizeQueryProfile(string(h.Profile))
}

// traceResourceToCode traces a resource back to its code repository.
// POST /api/v0/impact/trace-resource-to-code
// Body: {"start": "entity-id", "environment": "production", "max_depth": 8}
func (h *ImpactHandler) traceResourceToCode(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.resource_to_code") {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"resource-to-code tracing requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.resource_to_code",
			h.profile(),
			querycontract.RequiredProfile("platform_impact.resource_to_code"),
		)
		return
	}

	var req struct {
		Start       string `json:"start"`
		Environment string `json:"environment"`
		MaxDepth    int    `json:"max_depth"`
		Limit       int    `json:"limit"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Start == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "start is required")
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
	start, err := h.pathProbe().ResolveAnchor(r.Context(), h.Neo4j, "start_id", req.Start)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.resource_to_code") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	startInfo := map[string]any{"id": req.Start}
	paths := make([]map[string]any, 0)
	truncated := false
	if start != nil {
		startInfo = map[string]any{"id": start.ID, "name": start.Name, "labels": start.Labels}
		// Anchor the resolved label inline on the canonical id (indexed) and
		// project the raw relationships(path) list, unwound into per-hop
		// provenance in Go — the map-valued `[rel IN relationships(path) | {…}]`
		// comprehension is mangled on the pinned build.
		cypher := fmt.Sprintf(impacttrace.ImpactRepoPathCypher, start.Pattern("start", "start_id"), req.MaxDepth)
		rows, rerr := h.Neo4j.Run(r.Context(), cypher, map[string]any{"start_id": start.ID, "limit": limit + 1})
		if rerr != nil {
			if querycontract.WriteGraphReadError(w, r, rerr, "platform_impact.resource_to_code") {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, rerr.Error())
			return
		}
		var trimmed []map[string]any
		trimmed, truncated = trimImpactRows(rows, limit)
		for _, row := range trimmed {
			repoID := querycontract.StringVal(row, "repo_id")
			if repoID == "" {
				continue
			}
			paths = append(paths, map[string]any{
				"repo_id":   repoID,
				"repo_name": querycontract.StringVal(row, "repo_name"),
				"depth":     querycontract.IntVal(row, "depth"),
				"hops":      h.pathProbe().TraceHops(row["rels"]),
			})
		}
	}
	resp := map[string]any{"start": startInfo, "paths": paths, "count": len(paths), "limit": limit, "truncated": truncated}
	if req.Environment != "" {
		resp["environment"] = req.Environment
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, resp, querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.resource_to_code", querycontract.TruthBasisHybrid, "resolved from resource-to-code graph traversal"))
}

// explainDependencyPath finds and explains the shortest path between two entities.
// POST /api/v0/impact/explain-dependency-path
// Body: {"source": "entity-id", "target": "entity-id", "environment": "production"}
func (h *ImpactHandler) explainDependencyPath(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.dependency_path") {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dependency path analysis requires full dependency graph truth",
			"unsupported_capability",
			"platform_impact.dependency_path",
			h.profile(),
			querycontract.RequiredProfile("platform_impact.dependency_path"),
		)
		return
	}

	var req struct {
		Source      string `json:"source"`
		Target      string `json:"target"`
		Environment string `json:"environment"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Source == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "source is required")
		return
	}
	if req.Target == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "target is required")
		return
	}

	// Resolve the source and target labels with per-label inline-property anchors
	// (one CALL{UNION} each); a `MATCH (n:A|B|C) WHERE n.id = $id` label-
	// disjunction anchor matches zero rows on the pinned NornicDB build (#5286).
	sourceNode, err := h.pathProbe().ResolveAnchor(r.Context(), h.Neo4j, "source_id", req.Source)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.dependency_path") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	targetNode, err := h.pathProbe().ResolveAnchor(r.Context(), h.Neo4j, "target_id", req.Target)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.dependency_path") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sourceNode == nil || targetNode == nil {
		querycontract.WriteError(w, http.StatusNotFound, "source or target not found")
		return
	}

	source := map[string]any{"id": sourceNode.ID, "name": sourceNode.Name, "labels": sourceNode.Labels}
	target := map[string]any{"id": targetNode.ID, "name": targetNode.Name, "labels": targetNode.Labels}

	// Single anchoring clause: shortestPath with single-label inline-property
	// anchors on both ends, projecting the raw nodes(path)/relationships(path)
	// lists (zipped into hops in Go). The pinned build corrupts the old
	// two-disjunction-MATCH + WITH + map-valued rel comprehension shape.
	cypher := fmt.Sprintf(`MATCH path = shortestPath(%s-[*1..8]-%s)
RETURN length(path) AS depth, nodes(path) AS ns, relationships(path) AS rels`,
		sourceNode.Pattern("source", "source_id"), targetNode.Pattern("target", "target_id"))
	// Anchor on the resolved canonical ids (the caller may have passed a name).
	row, err := h.Neo4j.RunSingle(r.Context(), cypher, map[string]any{"source_id": sourceNode.ID, "target_id": targetNode.ID})
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.dependency_path") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
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
	if h.pathProbe().PathHasNodes(row["ns"]) {
		depth := querycontract.IntVal(row, "depth")
		pathInfo = map[string]any{"depth": depth}
		hops := h.pathProbe().DependencyHops(row["ns"], row["rels"])
		pathInfo["hops"] = hops

		confSum := 0.0
		confCount := 0
		reasons := []string{}
		for _, hop := range hops {
			if conf, ok := hop["confidence"].(float64); ok {
				confSum += conf
				confCount++
			}
			if reason := querycontract.StringVal(hop, "reason"); reason != "" {
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

	querycontract.WriteSuccess(w, r, http.StatusOK, resp, querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.dependency_path", querycontract.TruthBasisHybrid, "resolved from shortest-path dependency traversal"))
}
