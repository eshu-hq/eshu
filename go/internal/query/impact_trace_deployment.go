// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

type traceDeploymentChainRequest struct {
	ServiceName               string `json:"service_name"`
	DirectOnly                bool   `json:"direct_only"`
	MaxDepth                  int    `json:"max_depth"`
	IncludeRelatedModuleUsage bool   `json:"include_related_module_usage"`
}

type traceEnrichmentConfig struct {
	includeConsumers          bool
	includeProvisioningChains bool
	maxDepth                  int
}

func traceEnrichmentOptions(req traceDeploymentChainRequest) traceEnrichmentConfig {
	includeConsumers := !req.DirectOnly
	return traceEnrichmentConfig{
		includeConsumers:          includeConsumers,
		includeProvisioningChains: includeConsumers && req.IncludeRelatedModuleUsage,
		maxDepth:                  req.MaxDepth,
	}
}

// traceDeploymentChain returns a story-first deployment trace for a service.
// POST /api/v0/impact/trace-deployment-chain
func (h *ImpactHandler) traceDeploymentChain(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.deployment_chain") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"deployment-chain tracing requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.deployment_chain",
			h.profile(),
			requiredProfile("platform_impact.deployment_chain"),
		)
		return
	}

	var req traceDeploymentChainRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ServiceName == "" {
		WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}

	traceOptions := traceEnrichmentOptions(req)
	ctx, err := fetchServiceTraceContext(r.Context(), h.Neo4j, h.Content, h.Logger, req.ServiceName, traceOptions)
	if err != nil {
		if errors.Is(err, errAmbiguousTraceWorkloadSelector) {
			WriteError(w, http.StatusConflict, err.Error())
			return
		}
		if writeContentSubstringIndexUnavailable(w, err) {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if ctx == nil {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if workloadID := safeStr(ctx, "id"); workloadID != "" {
		deploymentSourceResult, err := h.fetchDeploymentSourceResult(r.Context(), workloadID, safeStr(ctx, "repo_id"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment sources: %v", err))
			return
		}
		deploymentSources := deploymentSourceResult.rows
		cloudResourceResult, err := h.fetchCloudResourceResult(
			r.Context(),
			safeStr(ctx, "repo_id"),
			workloadID,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query cloud resources: %v", err))
			return
		}
		cloudResources := cloudResourceResult.rows
		if len(cloudResources) == 0 {
			contextRows := mapSliceValue(ctx, "cloud_resources")
			contextRows, _ = capMapRows(contextRows, serviceStoryItemLimit)
			if len(contextRows) > 0 {
				cloudResourceResult.rows = contextRows
				cloudResourceResult.limits = nil
				cloudResources = cloudResourceResult.rows
			}
		}
		if len(cloudResources) == 0 && len(mapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
			configRows, configTruncated, configErr := loadConfigDerivedCloudResourceDependenciesBounded(
				r.Context(),
				h.Neo4j,
				mapValue(ctx, "deployment_evidence"),
				serviceStoryItemLimit,
			)
			if configErr != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query config-derived cloud resources: %v", configErr))
				return
			}
			if configTruncated {
				ctx["uncorrelated_cloud_resources_truncated"] = true
			}
			if len(configRows) > 0 && len(mapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
				ctx["uncorrelated_cloud_resources"] = deploymentTraceCloudCandidates(configRows)
			}
		}
		if len(cloudResources) > 0 {
			ctx["cloud_resources"] = cloudResources
			delete(ctx, "uncorrelated_cloud_resources")
		} else if len(mapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
			cloudCandidates, cloudCandidatesTruncated, err := loadUncorrelatedCloudResourceCandidatesBounded(
				r.Context(), h.Neo4j, safeStr(ctx, "name"), serviceStoryItemLimit,
			)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query uncorrelated cloud resources: %v", err))
				return
			}
			if len(cloudCandidates) > 0 {
				ctx["uncorrelated_cloud_resources"] = cloudCandidates
				if cloudCandidatesTruncated {
					ctx["uncorrelated_cloud_resources_truncated"] = true
				}
			}
		}
		k8sResourceResult, err := h.fetchK8sResourceResult(r.Context(), safeStr(ctx, "repo_id"), safeStr(ctx, "name"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query k8s resources: %v", err))
			return
		}
		deploymentSourceGitOps, err := h.fetchDeploymentSourceGitOpsResult(
			r.Context(),
			safeStr(ctx, "name"),
			safeStr(ctx, "repo_id"),
			deploymentSources,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment source gitops evidence: %v", err))
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
		imageRegistryTruth, err := h.fetchOCIImageRegistryTruth(r.Context(), imageRefs)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query OCI image registry truth: %v", err))
			return
		}
		ctx["deployment_sources"] = deploymentSources
		ctx["deployment_source_limits"] = deploymentSourceResult.limits
		ctx["cloud_resource_limits"] = cloudResourceResult.limits
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
		access := repositoryAccessFilterFromContext(r.Context())
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
		}
	}

	response := buildDeploymentTraceResponse(req.ServiceName, ctx)
	attachEvidenceBoundaries(response, "trace_deployment_chain")
	WriteSuccess(w, r, http.StatusOK, response, BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", TruthBasisHybrid, "resolved from deployment topology and service evidence"))
}

func fetchServiceTraceContext(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	logger *slog.Logger,
	serviceName string,
	traceOptions traceEnrichmentConfig,
) (map[string]any, error) {
	entityHandler := &EntityHandler{Neo4j: graph, Content: content, Logger: logger}
	workloadID, err := resolveTraceWorkloadSelector(ctx, graph, serviceName)
	if err != nil {
		return nil, err
	}
	var workloadContext map[string]any
	if workloadID != "" {
		workloadContext, err = entityHandler.fetchWorkloadContextForOperation(
			ctx,
			"w.id = $workload_id",
			map[string]any{"workload_id": workloadID},
			"deployment_trace",
		)
	} else {
		workloadContext, err = entityHandler.fetchServiceReadModelWorkloadContext(ctx, serviceName)
	}
	if err != nil || workloadContext == nil {
		return workloadContext, err
	}

	if err := enrichServiceQueryContextWithOptions(ctx, graph, content, workloadContext, serviceQueryEnrichmentOptions{
		DirectOnly:                !traceOptions.includeConsumers,
		IncludeRelatedModuleUsage: traceOptions.includeProvisioningChains,
		MaxDepth:                  traceOptions.maxDepth,
		Logger:                    logger,
		Operation:                 "deployment_trace",
	}); err != nil {
		return nil, fmt.Errorf("enrich service trace context: %w", err)
	}

	return workloadContext, nil
}
