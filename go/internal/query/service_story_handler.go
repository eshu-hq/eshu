package query

import (
	"fmt"
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

	ctx, err := h.fetchServiceWorkloadContext(r.Context(), serviceName, "service_story")
	if err != nil {
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

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		buildServiceStoryResponse(serviceName, ctx),
		BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			TruthBasisHybrid,
			"resolved from service dossier and platform evidence",
		),
	)
}
