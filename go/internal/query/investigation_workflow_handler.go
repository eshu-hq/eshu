// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// InvestigationWorkflowHandler exposes the deterministic investigation workflow
// catalog and resolver through the HTTP API. It reads only static catalog data.
type InvestigationWorkflowHandler struct {
	Profile QueryProfile
}

// Mount registers investigation workflow catalog and resolver routes.
func (h *InvestigationWorkflowHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/investigation-workflows", h.list)
	mux.HandleFunc("POST /api/v0/investigation-workflows/resolve", h.resolve)
}

type investigationWorkflowListResponse struct {
	SchemaVersion string                            `json:"schema_version"`
	Workflows     []InvestigationWorkflow           `json:"workflows"`
	Versions      []InvestigationWorkflowVersionRef `json:"versions"`
	Count         int                               `json:"count"`
}

type investigationWorkflowResolveRequest struct {
	WorkflowID      string            `json:"workflow_id"`
	Inputs          map[string]string `json:"inputs"`
	MissingEvidence []string          `json:"missing_evidence"`
}

type investigationWorkflowResolveResponse struct {
	SchemaVersion string                        `json:"schema_version"`
	Resolved      ResolvedInvestigationWorkflow `json:"resolved"`
}

func (h *InvestigationWorkflowHandler) list(w http.ResponseWriter, r *http.Request) {
	catalog := InvestigationWorkflowCatalog()
	WriteSuccess(w, r, http.StatusOK, investigationWorkflowListResponse{
		SchemaVersion: "investigation-workflows.v1",
		Workflows:     catalog,
		Versions:      InvestigationWorkflowCatalogVersions(),
		Count:         len(catalog),
	}, h.truth("deterministic guided investigation workflow catalog; no live backend read"))
}

func (h *InvestigationWorkflowHandler) resolve(w http.ResponseWriter, r *http.Request) {
	var req investigationWorkflowResolveRequest
	if err := ReadJSON(r, &req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "invalid investigation workflow resolve request")
		return
	}
	workflowID := strings.TrimSpace(req.WorkflowID)
	if workflowID == "" {
		h.writeError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "workflow_id is required")
		return
	}
	workflow, ok := LookupInvestigationWorkflow(workflowID)
	if !ok {
		h.writeError(w, r, http.StatusNotFound, ErrorCodeNotFound, "investigation workflow not found")
		return
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs:          req.Inputs,
		MissingEvidence: req.MissingEvidence,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, investigationWorkflowResolveResponse{
		SchemaVersion: "investigation-workflows.v1",
		Resolved:      resolved,
	}, h.truth("deterministic guided investigation workflow resolver; no live backend read"))
}

func (h *InvestigationWorkflowHandler) truth(reason string) *TruthEnvelope {
	return BuildTruthEnvelope(h.profile(), CapabilityInvestigationWorkflows, TruthBasisRuntimeState, reason)
}

func (h *InvestigationWorkflowHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data: nil,
			Error: &ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: CapabilityInvestigationWorkflows,
			},
		})
		return
	}
	WriteError(w, status, message)
}

func (h *InvestigationWorkflowHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}
