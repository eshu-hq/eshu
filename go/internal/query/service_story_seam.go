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

// ServiceWorkloadSelector is the exported selector for callers outside the query
// package that need the service-story dossier directly (for example the service
// intelligence report composer) rather than via the HTTP handler.
type ServiceWorkloadSelector struct {
	// ServiceName is the canonical service name. Required.
	ServiceName string
	// ServiceID narrows resolution to a specific service identifier.
	ServiceID string
	// Repository narrows resolution to a specific repository.
	Repository string
	// Environment narrows resolution to a specific environment.
	Environment string
}

// BuildServiceStoryEnvelope builds the service-story dossier and its truth
// envelope without writing HTTP. It is the reusable seam behind getServiceStory:
// the HTTP handler maps the returned (data, truth, status, errEnv) onto the wire,
// and other in-process composers (the service intelligence report) reuse the same
// truth-preserving build. It returns a non-nil errEnv with the matching HTTP
// status on every failure path — capability gate, invalid argument, no repository
// access, ambiguous or not-found selector, and internal enrichment errors — so
// callers never re-implement service resolution.
//
// The access-scope filter and capability gate are enforced here, so any caller
// inherits the same scoped-token and profile semantics as the HTTP route.
func (h *EntityHandler) BuildServiceStoryEnvelope(
	ctx context.Context,
	selector ServiceWorkloadSelector,
	operation string,
) (data map[string]any, truth *TruthEnvelope, status int, errEnv *ErrorEnvelope) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		return nil, nil, http.StatusNotImplemented, &ErrorEnvelope{
			Code:       ErrorCodeUnsupportedCapability,
			Message:    "service story requires authoritative platform context truth",
			Capability: "platform_impact.context_overview",
			Profiles:   &ErrorProfiles{Current: h.profile(), Required: requiredProfile("platform_impact.context_overview")},
		}
	}
	if selector.ServiceName == "" {
		return nil, nil, http.StatusBadRequest, &ErrorEnvelope{
			Code:       ErrorCodeInvalidArgument,
			Message:    "service_name is required",
			Capability: "platform_impact.context_overview",
		}
	}
	if repositoryAccessFilterFromContext(ctx).empty() {
		return nil, nil, http.StatusNotFound, serviceStoryNotFoundError()
	}

	workloadCtx, err := h.fetchServiceWorkloadContextWithSelector(ctx, serviceWorkloadSelector(selector), operation)
	if err != nil {
		status, errEnv := serviceStoryResolutionError(err)
		return nil, nil, status, errEnv
	}
	if workloadCtx == nil {
		return nil, nil, http.StatusNotFound, serviceStoryNotFoundError()
	}

	if err := enrichServiceQueryContextWithOptions(ctx, h.Neo4j, h.Content, workloadCtx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 operation,
	}); err != nil {
		if errors.Is(err, ErrContentSubstringIndexesNotReady) {
			return nil, nil, http.StatusServiceUnavailable, serviceStoryBackendUnavailableError(err)
		}
		if status, errEnv, ok := graphReadErrorEnvelope(err, "platform_impact.context_overview"); ok {
			return nil, nil, status, errEnv
		}
		return nil, nil, http.StatusInternalServerError, serviceStoryInternalError("enrich service story", err)
	}

	timer := startServiceQueryStage(ctx, h.Logger, operation, safeStr(workloadCtx, "name"), safeStr(workloadCtx, "repo_id"), "ci_cd_evidence")
	ciCDEvidence, err := loadRepositoryScopedCICDEvidence(ctx, h.Content, h.CICDRunCorrelations, safeStr(workloadCtx, "repo_id"))
	timer.Done(ctx, slog.Bool("has_result", len(ciCDEvidence) > 0), slog.Bool("error", err != nil))
	if err != nil {
		return nil, nil, http.StatusInternalServerError, serviceStoryInternalError("load service story ci/cd evidence", err)
	}
	if len(ciCDEvidence) > 0 {
		workloadCtx["ci_cd_evidence"] = ciCDEvidence
	}

	if h.ContainerImageIdentities != nil && h.SBOMAttachments != nil {
		timer := startServiceQueryStage(ctx, h.Logger, operation, safeStr(workloadCtx, "name"), safeStr(workloadCtx, "repo_id"), "supply_chain_evidence")
		if err := h.enrichServiceStorySupplyChainEvidence(ctx, workloadCtx); err != nil {
			timer.Done(ctx, slog.Bool("error", true))
			return nil, nil, http.StatusInternalServerError, serviceStoryInternalError("enrich service story supply chain evidence", err)
		}
		imagePackage := serviceStorySupplyChainImagePackage(workloadCtx)
		timer.Done(
			ctx,
			slog.Int("image_ref_count", len(StringSliceVal(imagePackage, "candidate_image_refs"))),
			slog.Int("evidence_count", len(mapSliceValue(imagePackage, "evidence"))),
			slog.Int("missing_count", len(StringSliceVal(imagePackage, "missing_evidence"))),
		)
	}

	data = buildServiceStoryResponse(selector.ServiceName, workloadCtx)
	truth = BuildTruthEnvelope(
		h.profile(),
		"platform_impact.context_overview",
		TruthBasisHybrid,
		"resolved from service dossier and platform evidence",
	)
	return data, truth, http.StatusOK, nil
}

// serviceStoryResolutionError maps a workload-resolution error to a status and
// error envelope, preserving the ambiguity candidate details and not-found
// classification the HTTP handler returns.
func serviceStoryResolutionError(err error) (int, *ErrorEnvelope) {
	var ambiguous serviceWorkloadAmbiguousError
	if errors.As(err, &ambiguous) {
		return http.StatusConflict, &ErrorEnvelope{
			Code:       ErrorCodeAmbiguous,
			Message:    ambiguous.Error(),
			Capability: "platform_impact.context_overview",
			Details: map[string]any{
				"status":     "ambiguous",
				"selector":   ambiguous.Selector,
				"candidates": serviceWorkloadCandidateMaps(ambiguous.Candidates),
				"truncated":  ambiguous.Truncated,
			},
		}
	}
	var repoAmbiguous repositorySelectorAmbiguousError
	if errors.As(err, &repoAmbiguous) {
		return http.StatusConflict, &ErrorEnvelope{
			Code:       ErrorCodeAmbiguous,
			Message:    repoAmbiguous.Error(),
			Capability: "platform_impact.context_overview",
			Details: map[string]any{
				"status":     "ambiguous",
				"selector":   repoAmbiguous.Selector,
				"candidates": repoAmbiguous.Matches,
				"truncated":  false,
			},
		}
	}
	if isRepositorySelectorNotFound(err) {
		return http.StatusNotFound, &ErrorEnvelope{
			Code:       ErrorCodeScopeNotFound,
			Message:    err.Error(),
			Capability: "platform_impact.context_overview",
		}
	}
	if status, errEnv, ok := graphReadErrorEnvelope(err, "platform_impact.context_overview"); ok {
		return status, errEnv
	}
	return http.StatusInternalServerError, serviceStoryInternalError("query failed", err)
}

func serviceStoryNotFoundError() *ErrorEnvelope {
	return &ErrorEnvelope{
		Code:       ErrorCodeNotFound,
		Message:    "service not found",
		Capability: "platform_impact.context_overview",
	}
}

func serviceStoryInternalError(prefix string, err error) *ErrorEnvelope {
	return &ErrorEnvelope{
		Code:       ErrorCodeInternalError,
		Message:    fmt.Sprintf("%s: %v", prefix, err),
		Capability: "platform_impact.context_overview",
	}
}

func serviceStoryBackendUnavailableError(err error) *ErrorEnvelope {
	return &ErrorEnvelope{
		Code:       ErrorCodeBackendUnavailable,
		Message:    err.Error(),
		Capability: "platform_impact.context_overview",
	}
}
