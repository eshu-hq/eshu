// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type traceDeploymentChainRequest struct {
	ServiceName               string `json:"service_name"`
	DirectOnly                bool   `json:"direct_only"`
	MaxDepth                  int    `json:"max_depth"`
	IncludeRelatedModuleUsage bool   `json:"include_related_module_usage"`
}

// traceDeploymentChainMaxDepthLimit bounds traceDeploymentChainRequest.MaxDepth
// at the HTTP boundary. #5720 P2-3: boundedTraceEnrichmentLimit (which derives
// an indirect-evidence search limit from this field via maxDepth*10) now
// clamps its own output, but an absurd input -- negative, or large enough to
// read as abuse or a caller bug rather than an intentional depth -- is
// normalized here rather than silently reinterpreted deeper in the call
// chain. #5720 round-2 P1-2: the first draft of this boundary rejected
// out-of-range values with 400, which broke every sibling max_depth-bearing
// route's contract (impact_resource_investigation.go,
// impact_change_surface_investigation.go, impact_change_surface_legacy.go,
// and this route's own OpenAPI fragment all normalize rather than reject) and
// changed observable behavior for existing callers: max_depth=5000 used to
// 200 with output identical to max_depth=1000 (both saturate
// boundedTraceEnrichmentLimit at 100), and max_depth=-1 used to 200 using the
// default limit of 25. The MCP dispatch route (dispatch_impact.go) passes an
// explicit max_depth straight through from `intOr`, so an out-of-range value
// there would have 400'd a caller that previously got results, for no
// additional safety -- boundedTraceEnrichmentLimit already maps every int
// input, including MaxInt64/MinInt64 and both multiply-overflow wrap points,
// into (0, maxIndirectEvidenceSearchLimit]. Clamping instead of rejecting
// restores that contract while keeping the boundary: the limit stays
// generous relative to boundedTraceEnrichmentLimit's own saturation point
// (any max_depth above 10 already yields the same clamped limit), while
// staying far below the point where maxDepth*10 would overflow int64.
const traceDeploymentChainMaxDepthLimit = 1000

// normalizeTraceDeploymentChainMaxDepth clamps a caller-supplied max_depth to
// [0, traceDeploymentChainMaxDepthLimit]: negative values (including
// math.MinInt) clamp to zero, values above the limit (including
// math.MaxInt and overflow-scale inputs such as 922337203685477581) clamp
// down to the limit, and in-range values pass through unchanged. Extracted
// as a pure function, mirroring normalizeChangeSurfaceLegacyDepth
// (impact_change_surface_legacy.go) and the MaxDepth clamp in
// ResourceInvestigationRequest.normalize (impact_resource_investigation.go),
// so the boundary itself has direct unit coverage independent of
// boundedTraceEnrichmentLimit's own saturation, which maps every int input
// into (0, maxIndirectEvidenceSearchLimit] regardless of whether this clamp
// ran. #5720 round-4 P2.
func normalizeTraceDeploymentChainMaxDepth(maxDepth int) int {
	if maxDepth < 0 {
		return 0
	}
	if maxDepth > traceDeploymentChainMaxDepthLimit {
		return traceDeploymentChainMaxDepthLimit
	}
	return maxDepth
}

type TraceEnrichmentConfig struct {
	IncludeConsumers          bool
	IncludeProvisioningChains bool
	MaxDepth                  int
}

func traceEnrichmentOptions(req traceDeploymentChainRequest) TraceEnrichmentConfig {
	includeConsumers := !req.DirectOnly
	return TraceEnrichmentConfig{
		IncludeConsumers:          includeConsumers,
		IncludeProvisioningChains: includeConsumers && req.IncludeRelatedModuleUsage,
		MaxDepth:                  req.MaxDepth,
	}
}

// TraceDeploymentChain returns a story-first deployment trace for a service.
// POST /api/v0/impact/trace-deployment-chain
func (h *ImpactHandler) TraceDeploymentChain(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.deployment_chain") {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"deployment-chain tracing requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.deployment_chain",
			h.profile(),
			querycontract.RequiredProfile("platform_impact.deployment_chain"),
		)
		return
	}

	var req traceDeploymentChainRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ServiceName == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}
	req.MaxDepth = normalizeTraceDeploymentChainMaxDepth(req.MaxDepth)

	traceOptions := traceEnrichmentOptions(req)
	ctx, err := h.traceContext().FetchServiceTraceContext(r.Context(), h.Neo4j, h.Content, h.Logger, req.ServiceName, traceOptions)
	if err != nil {
		if errors.Is(err, impacttrace.ErrAmbiguousTraceWorkloadSelector) {
			querycontract.WriteError(w, http.StatusConflict, err.Error())
			return
		}
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.deployment_chain") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if ctx == nil {
		querycontract.WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if workloadID := querycontract.SafeStr(ctx, "id"); workloadID != "" {
		deploymentSourceResult, err := h.FetchDeploymentSourceResult(r.Context(), workloadID, querycontract.SafeStr(ctx, "repo_id"))
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, "platform_impact.deployment_chain") {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment sources: %v", err))
			return
		}
		deploymentSources := deploymentSourceResult.rows
		cloudResourceResult, err := h.fetchCloudResourceResult(
			r.Context(),
			querycontract.SafeStr(ctx, "repo_id"),
			workloadID,
		)
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, "platform_impact.deployment_chain") {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query cloud resources: %v", err))
			return
		}
		cloudResources := cloudResourceResult.Rows
		if len(cloudResources) == 0 {
			contextRows := querycontract.MapSliceValue(ctx, "cloud_resources")
			contextRows, _ = querycontract.CapMapRows(contextRows, querycontract.ServiceStoryItemLimit)
			if len(contextRows) > 0 {
				cloudResourceResult.Rows = contextRows
				cloudResourceResult.Limits = nil
				cloudResources = cloudResourceResult.Rows
			}
		}
		if len(cloudResources) == 0 && len(querycontract.MapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
			configRows, configTruncated, configErr := impacttrace.LoadConfigDerivedCloudResourceDependenciesBounded(
				r.Context(),
				h.Neo4j,
				querycontract.MapValue(ctx, "deployment_evidence"),
				querycontract.ServiceStoryItemLimit,
			)
			if configErr != nil {
				if querycontract.WriteGraphReadError(w, r, configErr, "platform_impact.deployment_chain") {
					return
				}
				querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query config-derived cloud resources: %v", configErr))
				return
			}
			if configTruncated {
				ctx["uncorrelated_cloud_resources_truncated"] = true
			}
			if len(configRows) > 0 && len(querycontract.MapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
				ctx["uncorrelated_cloud_resources"] = impacttrace.DeploymentTraceCloudCandidates(configRows)
			}
		}
		if len(cloudResources) > 0 {
			ctx["cloud_resources"] = cloudResources
			delete(ctx, "uncorrelated_cloud_resources")
		} else if len(querycontract.MapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
			cloudCandidates, cloudCandidatesTruncated, err := impacttrace.LoadUncorrelatedCloudResourceCandidatesBounded(
				r.Context(), h.Neo4j, querycontract.SafeStr(ctx, "name"), querycontract.ServiceStoryItemLimit,
			)
			if err != nil {
				if querycontract.WriteGraphReadError(w, r, err, "platform_impact.deployment_chain") {
					return
				}
				querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query uncorrelated cloud resources: %v", err))
				return
			}
			if len(cloudCandidates) > 0 {
				ctx["uncorrelated_cloud_resources"] = cloudCandidates
				if cloudCandidatesTruncated {
					ctx["uncorrelated_cloud_resources_truncated"] = true
				}
			}
		}
		k8sResourceResult, err := h.FetchK8sResourceResult(r.Context(), querycontract.SafeStr(ctx, "repo_id"), querycontract.SafeStr(ctx, "name"))
		if err != nil {
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query k8s resources: %v", err))
			return
		}
		deploymentSourceGitOps, err := h.fetchDeploymentSourceGitOpsResult(
			r.Context(),
			querycontract.SafeStr(ctx, "name"),
			querycontract.SafeStr(ctx, "repo_id"),
			deploymentSources,
		)
		if err != nil {
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment source gitops evidence: %v", err))
			return
		}
		k8sResourceResult = boundedK8sResourceResult(
			k8sResourceResult.candidates,
			k8sResourceResult.contentLowerBound,
			deploymentSourceGitOps.k8sResources,
			deploymentSourceGitOps.k8sObservedCountIsLowerBound,
			k8sResourceResult.selectCandidatePoolTruncated,
		)
		k8sResources := k8sResourceResult.rows
		imageRefs := k8sResourceResult.imageRefs
		imageRegistryTruth, err := h.FetchOCIImageRegistryTruth(r.Context(), imageRefs)
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, "platform_impact.deployment_chain") {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query OCI image registry truth: %v", err))
			return
		}
		ctx["deployment_sources"] = deploymentSources
		ctx["deployment_source_limits"] = deploymentSourceResult.limits
		ctx["cloud_resource_limits"] = cloudResourceResult.Limits
		ctx["k8s_resource_limits"] = k8sResourceResult.limits
		if len(cloudResources) > 0 {
			ctx["cloud_resources"] = cloudResources
		}
		ctx["k8s_resources"] = k8sResources
		ctx["image_refs"] = imageRefs
		if len(imageRegistryTruth) > 0 {
			ctx["image_registry_truth"] = imageRegistryTruth
		}
		ctx["controller_entities"] = deploymentSourceGitOps.controllers
		ctx["controller_entity_limits"] = deploymentSourceGitOps.controllerLimits

		// D2 (#5471): re-derive the repository access filter for the
		// live-evidence probe. #5530 removed the handler's earlier
		// access uses as redundant with fetchWorkloadContext filtering,
		// but this probe is a separate scope-sensitive Postgres read
		// (#5167 discipline) and must carry the caller's grant set — a
		// zero-value filter would read all scopes cross-tenant.
		access := querycontract.RepositoryAccessFilterFromContext(r.Context())
		// D2 (#5471), rebound to identity in the codex P1 fix: probe for a
		// live kubernetes_live.pod_template fact whose ArgoCD tracking-id
		// matches an identity the traced workload's OWN declared ArgoCD
		// Application + k8sResources would carry, and whose image_refs
		// intersect the workload's config-declared image refs. An
		// identity-bound match means a live cluster observably runs THIS
		// workload's declared image — that promotes the deployment truth
		// tier from config_only to runtime_confirmed. A shared image digest
		// alone (the pre-fix behavior) is no longer sufficient.
		liveEvidence, err := h.fetchWorkloadLiveEvidence(
			r.Context(),
			deploymentSourceGitOps.controllers,
			k8sResources,
			imageRefs,
			access,
		)
		if err != nil {
			// Store errors fail closed to the config tier.
			// Record the live-evidence probe failed but do not
			// 500 the whole trace.
			if h.Logger != nil {
				h.Logger.Warn(
					"impact handler: live evidence probe failed, falling back to config tier",
					"service_name", req.ServiceName,
					"error", err.Error(),
				)
			}
		}
		ctx["_has_live_evidence"] = liveEvidence

		// #5638: read-side live_instance_count, over the SAME identity-bound
		// facts the probe above just checked for existence -- a separate
		// probe because it needs the actual matched rows (ready_replicas),
		// not a bare existence bool. Errors
		// log-and-continue exactly like the live-evidence probe above: a
		// count failure must not 500 the trace and must never touch
		// _has_live_evidence (this probe never writes that key).
		liveInstances, err := h.fetchWorkloadLiveInstanceSummary(
			r.Context(),
			deploymentSourceGitOps.controllers,
			k8sResources,
			imageRefs,
			access,
		)
		if err != nil {
			if h.Logger != nil {
				h.Logger.Warn(
					"impact handler: live instance count probe failed, omitting count",
					"service_name", req.ServiceName,
					"error", err.Error(),
				)
			}
		} else if liveInstances != nil {
			ctx["_live_instance_count"] = liveInstances.count
			// #5663: attached alongside the count so the response can
			// disclose whether an anchor's read hit querycontract.ServiceStoryItemLimit
			// rows -- the count is then a conservative lower bound, not an
			// exact total. Only set when liveInstances is non-nil,
			// mirroring how _live_instance_count itself is only ever set on
			// an actual observation.
			ctx["_live_instance_count_truncated"] = liveInstances.truncated
		}
	}

	response := impacttrace.BuildDeploymentTraceResponse(req.ServiceName, ctx, h.traceContext().BuildServiceDeploymentOverview(ctx))
	querycontract.AttachEvidenceBoundaries(response, "trace_deployment_chain")
	querycontract.WriteSuccess(w, r, http.StatusOK, response, querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", querycontract.TruthBasisHybrid, "resolved from deployment topology and service evidence"))
}
