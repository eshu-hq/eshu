package query

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// getServiceStory retrieves a complete dossier for a service.
func (h *EntityHandler) getServiceStory(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service story requires authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			requiredProfile("platform_impact.context_overview"),
		)
		return
	}

	serviceName := PathParam(r, "service_name")
	if serviceName == "" {
		if acceptsEnvelope(r) {
			WriteJSON(w, http.StatusBadRequest, ResponseEnvelope{
				Data: nil,
				Error: &ErrorEnvelope{
					Code:       ErrorCodeInvalidArgument,
					Message:    "service_name is required",
					Capability: "platform_impact.context_overview",
				},
			})
			return
		}
		WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}
	if repositoryAccessFilterFromContext(r.Context()).empty() {
		if acceptsEnvelope(r) {
			WriteJSON(w, http.StatusNotFound, ResponseEnvelope{
				Data: nil,
				Error: &ErrorEnvelope{
					Code:       ErrorCodeNotFound,
					Message:    "service not found",
					Capability: "platform_impact.context_overview",
				},
			})
			return
		}
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}

	ctx, err := h.fetchServiceWorkloadContextWithSelector(r.Context(), serviceWorkloadSelector{
		ServiceName: serviceName,
		ServiceID:   QueryParam(r, "service_id"),
		Repository:  QueryParam(r, "repo"),
		Environment: QueryParam(r, "environment"),
	}, "service_story")
	if err != nil {
		var ambiguous serviceWorkloadAmbiguousError
		if errors.As(err, &ambiguous) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusConflict,
				ErrorCodeAmbiguous,
				ambiguous.Error(),
				map[string]any{
					"status":     "ambiguous",
					"selector":   ambiguous.Selector,
					"candidates": serviceWorkloadCandidateMaps(ambiguous.Candidates),
					"truncated":  ambiguous.Truncated,
				},
			)
			return
		}
		var repoAmbiguous repositorySelectorAmbiguousError
		if errors.As(err, &repoAmbiguous) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusConflict,
				ErrorCodeAmbiguous,
				repoAmbiguous.Error(),
				map[string]any{
					"status":     "ambiguous",
					"selector":   repoAmbiguous.Selector,
					"candidates": repoAmbiguous.Matches,
					"truncated":  false,
				},
			)
			return
		}
		if isRepositorySelectorNotFound(err) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusNotFound,
				ErrorCodeScopeNotFound,
				err.Error(),
				nil,
			)
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		if acceptsEnvelope(r) {
			WriteJSON(w, http.StatusNotFound, ResponseEnvelope{
				Data: nil,
				Error: &ErrorEnvelope{
					Code:       ErrorCodeNotFound,
					Message:    "service not found",
					Capability: "platform_impact.context_overview",
				},
			})
			return
		}
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := enrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "service_story",
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service story: %v", err))
		return
	}
	timer := startServiceQueryStage(r.Context(), h.Logger, "service_story", safeStr(ctx, "name"), safeStr(ctx, "repo_id"), "ci_cd_evidence")
	ciCDEvidence, err := loadRepositoryScopedCICDEvidence(r.Context(), h.Content, h.CICDRunCorrelations, safeStr(ctx, "repo_id"))
	timer.Done(r.Context(), slog.Bool("has_result", len(ciCDEvidence) > 0), slog.Bool("error", err != nil))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load service story ci/cd evidence: %v", err))
		return
	}
	if len(ciCDEvidence) > 0 {
		ctx["ci_cd_evidence"] = ciCDEvidence
	}
	if h.ContainerImageIdentities != nil && h.SBOMAttachments != nil {
		timer := startServiceQueryStage(r.Context(), h.Logger, "service_story", safeStr(ctx, "name"), safeStr(ctx, "repo_id"), "supply_chain_evidence")
		if err := h.enrichServiceStorySupplyChainEvidence(r.Context(), ctx); err != nil {
			timer.Done(r.Context(), slog.Bool("error", true))
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service story supply chain evidence: %v", err))
			return
		}
		imagePackage := serviceStorySupplyChainImagePackage(ctx)
		timer.Done(r.Context(),
			slog.Int("image_ref_count", len(StringSliceVal(imagePackage, "candidate_image_refs"))),
			slog.Int("evidence_count", len(mapSliceValue(imagePackage, "evidence"))),
			slog.Int("missing_count", len(StringSliceVal(imagePackage, "missing_evidence"))),
		)
	}

	data := buildServiceStoryResponse(serviceName, ctx)
	truth := BuildTruthEnvelope(
		h.profile(),
		"platform_impact.context_overview",
		TruthBasisHybrid,
		"resolved from service dossier and platform evidence",
	)
	WriteSuccess(w, r, http.StatusOK, serviceStoryAnswerData(serviceName, data, truth), truth)
}

func writeServiceStoryEnvelopeError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code ErrorCode,
	message string,
	data any,
) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data:  nil,
			Truth: nil,
			Error: &ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: "platform_impact.context_overview",
				Details:    serviceStoryErrorDetails(data),
			},
		})
		return
	}
	WriteError(w, status, message)
}

func serviceStoryErrorDetails(data any) map[string]any {
	if details, ok := data.(map[string]any); ok {
		return details
	}
	return nil
}
