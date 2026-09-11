// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// getServiceStory retrieves a complete dossier for a service. It is a thin HTTP
// wrapper over BuildServiceStoryEnvelope: the dossier build, truth, and all
// error/ambiguity classification live in the reusable seam, so the HTTP route and
// in-process composers (the service intelligence report) share one truth path.
// GetServiceStory serves the service-story route. Exported so the staying service-context authz test keeps driving the handler; see #6060.
func (h *EntityHandler) GetServiceStory(w http.ResponseWriter, r *http.Request) {
	serviceName := querycontract.PathParam(r, "service_name")
	data, truth, status, errEnv := h.BuildServiceStoryEnvelope(r.Context(), service.WorkloadSelector{
		ServiceName: serviceName,
		ServiceID:   querycontract.QueryParam(r, "service_id"),
		Repository:  querycontract.QueryParam(r, "repo"),
		Environment: querycontract.QueryParam(r, "environment"),
	}, "service_story")
	if errEnv != nil {
		querycontract.WriteErrorEnvelope(w, r, status, errEnv)
		return
	}
	querycontract.WriteSuccess(w, r, status, querycontract.ServiceStoryAnswerData(serviceName, data, truth), truth)
}

func writeServiceStoryEnvelopeError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code querycontract.ErrorCode,
	message string,
	data any,
) {
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteJSON(w, status, querycontract.ResponseEnvelope{
			Data:  nil,
			Truth: nil,
			Error: &querycontract.ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: "platform_impact.context_overview",
				Details:    serviceStoryErrorDetails(data),
			},
		})
		return
	}
	querycontract.WriteError(w, status, message)
}

func serviceStoryErrorDetails(data any) map[string]any {
	if details, ok := data.(map[string]any); ok {
		return details
	}
	return nil
}
