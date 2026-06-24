// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// getServiceStory retrieves a complete dossier for a service. It is a thin HTTP
// wrapper over BuildServiceStoryEnvelope: the dossier build, truth, and all
// error/ambiguity classification live in the reusable seam, so the HTTP route and
// in-process composers (the service intelligence report) share one truth path.
func (h *EntityHandler) getServiceStory(w http.ResponseWriter, r *http.Request) {
	serviceName := PathParam(r, "service_name")
	data, truth, status, errEnv := h.BuildServiceStoryEnvelope(r.Context(), ServiceWorkloadSelector{
		ServiceName: serviceName,
		ServiceID:   QueryParam(r, "service_id"),
		Repository:  QueryParam(r, "repo"),
		Environment: QueryParam(r, "environment"),
	}, "service_story")
	if errEnv != nil {
		WriteErrorEnvelope(w, r, status, errEnv)
		return
	}
	WriteSuccess(w, r, status, serviceStoryAnswerData(serviceName, data, truth), truth)
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
