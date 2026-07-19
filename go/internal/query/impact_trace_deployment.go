// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
		// #5167 W3: the workload identity itself is already bound to the
		// caller's grant (fetchServiceTraceContext -> fetchServiceWorkloadContext
		// -> fetchWorkloadContextForOperation applies repositoryAccessFilterFromContext),
		// but the enrichment below reads other repositories' deployment-source
		// edges, so deploymentSources is independently bound to the grant and
		// every downstream read that derives repo ids from it inherits the
		// filtered set.
		access := repositoryAccessFilterFromContext(r.Context())
		deploymentSources, err := h.fetchDeploymentSources(r.Context(), workloadID, safeStr(ctx, "repo_id"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment sources: %v", err))
			return
		}
		deploymentSources = filterRowsByRepoIDForAccess(deploymentSources, access)
		cloudResources, err := h.fetchCloudResources(r.Context(), workloadID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query cloud resources: %v", err))
			return
		}
		if len(cloudResources) == 0 {
			cloudResources = mapSliceValue(ctx, "cloud_resources")
		}
		// The config-derived and "uncorrelated" cloud-resource fallbacks below
		// are free-text CloudResource scans with no repo_id in their result rows
		// at all (#5167 W3) -- there is no property to bind to the caller's
		// grant, so a scoped caller never runs them and simply sees no fallback
		// cloud resources rather than a cross-tenant free-text leak.
		if len(cloudResources) == 0 && !access.scoped() {
			cloudResources, err = loadConfigDerivedCloudResourceDependencies(
				r.Context(),
				h.Neo4j,
				mapValue(ctx, "deployment_evidence"),
				serviceStoryItemLimit,
			)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query config-derived cloud resources: %v", err))
				return
			}
		}
		if len(cloudResources) > 0 {
			ctx["cloud_resources"] = cloudResources
			delete(ctx, "uncorrelated_cloud_resources")
		} else if len(mapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 && !access.scoped() {
			cloudCandidates, err := loadUncorrelatedCloudResourceCandidates(r.Context(), h.Neo4j, safeStr(ctx, "name"), serviceStoryItemLimit)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query uncorrelated cloud resources: %v", err))
				return
			}
			if len(cloudCandidates) > 0 {
				ctx["uncorrelated_cloud_resources"] = cloudCandidates
			}
		}
		k8sResources, imageRefs, err := h.fetchK8sResources(r.Context(), safeStr(ctx, "repo_id"), safeStr(ctx, "name"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query k8s resources: %v", err))
			return
		}
		controllerEntities, deploymentRepoK8s, deploymentRepoImages, err := h.fetchDeploymentSourceGitOps(
			r.Context(),
			safeStr(ctx, "name"),
			deploymentSources,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment source gitops evidence: %v", err))
			return
		}
		k8sResources = mergeDeploymentTraceRows(k8sResources, deploymentRepoK8s)
		imageRefs = uniqueSortedStrings(append(append([]string{}, imageRefs...), deploymentRepoImages...))
		imageRegistryTruth, err := h.fetchOCIImageRegistryTruth(r.Context(), imageRefs)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query OCI image registry truth: %v", err))
			return
		}
		ctx["deployment_sources"] = deploymentSources
		if len(cloudResources) > 0 {
			ctx["cloud_resources"] = cloudResources
		}
		ctx["k8s_resources"] = k8sResources
		ctx["image_refs"] = imageRefs
		if len(imageRegistryTruth) > 0 {
			ctx["image_registry_truth"] = imageRegistryTruth
		}
		ctx["controller_entities"] = controllerEntities
	}

	WriteSuccess(w, r, http.StatusOK, buildDeploymentTraceResponse(req.ServiceName, ctx), BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", TruthBasisHybrid, "resolved from deployment topology and service evidence"))
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
	workloadContext, err := entityHandler.fetchServiceWorkloadContext(ctx, serviceName, "deployment_trace")
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

func (h *ImpactHandler) fetchDeploymentSources(
	ctx context.Context,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	return fetchDeploymentSourcesFromGraph(ctx, h.Neo4j, workloadID, repoID)
}

func fetchDeploymentSourcesFromGraph(
	ctx context.Context,
	reader GraphQuery,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	canonicalRows, err := reader.Run(ctx, `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)
		RETURN DISTINCT repo.id as repo_id, repo.name as repo_name, rel.confidence as confidence, rel.reason as reason
		ORDER BY repo_name
	`, map[string]any{
		"workload_id": workloadID,
	})
	if err != nil {
		return nil, err
	}
	repositoryRows := []map[string]any{}
	if strings.TrimSpace(repoID) != "" {
		repositoryRows, err = reader.Run(ctx, `
			MATCH (targetRepo:Repository {id: $repo_id})<-[rel:DEPLOYS_FROM]-(repo:Repository)
			RETURN DISTINCT repo.id as repo_id, repo.name as repo_name, rel.confidence as confidence,
			       coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason
			ORDER BY repo_name
		`, map[string]any{
			"repo_id": repoID,
		})
		if err != nil {
			return nil, err
		}
	}
	rows := mergeDeploymentSourceRows(canonicalRows, repositoryRows)
	sources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, map[string]any{
			"repo_id":    StringVal(row, "repo_id"),
			"repo_name":  StringVal(row, "repo_name"),
			"confidence": floatVal(row, "confidence"),
			"reason":     StringVal(row, "reason"),
		})
	}
	return sources, nil
}

func mergeDeploymentSourceRows(
	canonicalRows []map[string]any,
	repositoryRows []map[string]any,
) []map[string]any {
	merged := make([]map[string]any, 0, len(canonicalRows)+len(repositoryRows))
	seen := make(map[string]struct{}, len(canonicalRows)+len(repositoryRows))
	appendRow := func(row map[string]any) {
		key := StringVal(row, "repo_id")
		if key == "" {
			key = StringVal(row, "repo_name")
		}
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, row)
	}
	for _, row := range canonicalRows {
		appendRow(row)
	}
	for _, row := range repositoryRows {
		appendRow(row)
	}
	return merged
}
