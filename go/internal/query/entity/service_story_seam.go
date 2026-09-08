// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"

	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

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
	selector service.ServiceWorkloadSelector,
	operation string,
) (data map[string]any, truth *querycontract.TruthEnvelope, status int, errEnv *querycontract.ErrorEnvelope) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		return nil, nil, http.StatusNotImplemented, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeUnsupportedCapability,
			Message:    "service story requires authoritative platform context truth",
			Capability: "platform_impact.context_overview",
			Profiles:   &querycontract.ErrorProfiles{Current: h.profile(), Required: querycontract.RequiredProfile("platform_impact.context_overview")},
		}
	}
	if selector.ServiceName == "" {
		return nil, nil, http.StatusBadRequest, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeInvalidArgument,
			Message:    "service_name is required",
			Capability: "platform_impact.context_overview",
		}
	}
	if querycontract.RepositoryAccessFilterFromContext(ctx).Empty() {
		return nil, nil, http.StatusNotFound, serviceStoryNotFoundError()
	}

	workloadCtx, err := h.fetchServiceWorkloadContextWithSelector(ctx, ServiceWorkloadSelector(selector), operation)
	if err != nil {
		status, errEnv := serviceStoryResolutionError(err)
		return nil, nil, status, errEnv
	}
	if workloadCtx == nil {
		return nil, nil, http.StatusNotFound, serviceStoryNotFoundError()
	}

	if err := service.EnrichServiceQueryContextWithOptions(ctx, h.Neo4j, h.Content, workloadCtx, service.ServiceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 operation,
	}); err != nil {
		if errors.Is(err, querycontract.ErrContentSubstringIndexesNotReady) {
			return nil, nil, http.StatusServiceUnavailable, serviceStoryBackendUnavailableError(err)
		}
		if status, errEnv, ok := querycontract.GraphReadErrorEnvelope(err, "platform_impact.context_overview"); ok {
			return nil, nil, status, errEnv
		}
		return nil, nil, http.StatusInternalServerError, serviceStoryInternalError("enrich service story", err)
	}

	timer := service.StartServiceQueryStage(ctx, h.Logger, operation, safeStr(workloadCtx, "name"), safeStr(workloadCtx, "repo_id"), "ci_cd_evidence")
	ciCDEvidence, err := artifacts.LoadRepositoryScopedCICDEvidence(ctx, h.Content, h.CICDRunCorrelations, safeStr(workloadCtx, "repo_id"))
	timer.Done(ctx, slog.Bool("has_result", len(ciCDEvidence) > 0), slog.Bool("error", err != nil))
	if err != nil {
		return nil, nil, http.StatusInternalServerError, serviceStoryInternalError("load service story ci/cd evidence", err)
	}
	if len(ciCDEvidence) > 0 {
		workloadCtx["ci_cd_evidence"] = ciCDEvidence
	}

	if h.ContainerImageIdentities != nil && h.SBOMAttachments != nil {
		timer := service.StartServiceQueryStage(ctx, h.Logger, operation, safeStr(workloadCtx, "name"), safeStr(workloadCtx, "repo_id"), "supply_chain_evidence")
		if err := h.enrichServiceStorySupplyChainEvidence(ctx, workloadCtx); err != nil {
			timer.Done(ctx, slog.Bool("error", true))
			return nil, nil, http.StatusInternalServerError, serviceStoryInternalError("enrich service story supply chain evidence", err)
		}
		imagePackage := service.ServiceStorySupplyChainImagePackage(workloadCtx)
		timer.Done(
			ctx,
			slog.Int("image_ref_count", len(querycontract.StringSliceVal(imagePackage, "candidate_image_refs"))),
			slog.Int("evidence_count", len(querycontract.MapSliceValue(imagePackage, "evidence"))),
			slog.Int("missing_count", len(querycontract.StringSliceVal(imagePackage, "missing_evidence"))),
		)
	}

	data = service.BuildServiceStoryResponse(selector.ServiceName, workloadCtx)
	truth = querycontract.BuildTruthEnvelope(
		h.profile(),
		"platform_impact.context_overview",
		querycontract.TruthBasisHybrid,
		"resolved from service dossier and platform evidence",
	)
	return data, truth, http.StatusOK, nil
}

// serviceStoryResolutionError maps a workload-resolution error to a status and
// error envelope, preserving the ambiguity candidate details and not-found
// classification the HTTP handler returns.
func serviceStoryResolutionError(err error) (int, *querycontract.ErrorEnvelope) {
	var ambiguous serviceWorkloadAmbiguousError
	if errors.As(err, &ambiguous) {
		return http.StatusConflict, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeAmbiguous,
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
	var repoAmbiguous queryselector.AmbiguousError
	if errors.As(err, &repoAmbiguous) {
		return http.StatusConflict, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeAmbiguous,
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
	if queryselector.IsNotFound(err) {
		return http.StatusNotFound, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeScopeNotFound,
			Message:    err.Error(),
			Capability: "platform_impact.context_overview",
		}
	}
	if status, errEnv, ok := querycontract.GraphReadErrorEnvelope(err, "platform_impact.context_overview"); ok {
		return status, errEnv
	}
	return http.StatusInternalServerError, serviceStoryInternalError("query failed", err)
}

func serviceStoryNotFoundError() *querycontract.ErrorEnvelope {
	return &querycontract.ErrorEnvelope{
		Code:       querycontract.ErrorCodeNotFound,
		Message:    "service not found",
		Capability: "platform_impact.context_overview",
	}
}

func serviceStoryInternalError(prefix string, err error) *querycontract.ErrorEnvelope {
	return &querycontract.ErrorEnvelope{
		Code:       querycontract.ErrorCodeInternalError,
		Message:    fmt.Sprintf("%s: %v", prefix, err),
		Capability: "platform_impact.context_overview",
	}
}

func serviceStoryBackendUnavailableError(err error) *querycontract.ErrorEnvelope {
	return &querycontract.ErrorEnvelope{
		Code:       querycontract.ErrorCodeBackendUnavailable,
		Message:    err.Error(),
		Capability: "platform_impact.context_overview",
	}
}
