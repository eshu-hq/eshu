// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	investigationWorkflowDefaultLimit = 20
	investigationWorkflowMaxLimit     = 200
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
	Workflows     any                               `json:"workflows"`
	Versions      []InvestigationWorkflowVersionRef `json:"versions"`
	Count         int                               `json:"count"`
	Total         int                               `json:"total"`
	Limit         int                               `json:"limit"`
	Offset        int                               `json:"offset"`
	Truncated     bool                              `json:"truncated"`
	NextOffset    any                               `json:"next_offset"`
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

// list returns the investigation workflow catalog with deterministic
// limit/offset paging. The default response is the compact view: each
// workflow is an InvestigationWorkflowSummary (id/name/version/domain/
// description), omitting tool groups, required/optional evidence, and
// missing-evidence routes, which dominate a workflow's serialized size
// (#6795). Pass view=full for the complete workflow list.
// GET /api/v0/investigation-workflows?limit=&offset=&view=
func (h *InvestigationWorkflowHandler) list(w http.ResponseWriter, r *http.Request) {
	catalog := InvestigationWorkflowCatalog()

	limit, ok := querycontract.ParseBoundedLimit(w, r, investigationWorkflowDefaultLimit, investigationWorkflowMaxLimit)
	if !ok {
		return
	}
	offset, ok := querycontract.ParseOffset(w, r)
	if !ok {
		return
	}
	full, ok := querycontract.ParseCatalogView(w, r)
	if !ok {
		return
	}

	total := len(catalog)
	page, truncated := pageInvestigationWorkflows(catalog, offset, limit)

	var workflows any
	if full {
		workflows = page
	} else {
		summaries := make([]InvestigationWorkflowSummary, len(page))
		for i, workflow := range page {
			summaries[i] = workflow.ToSummary()
		}
		workflows = summaries
	}

	WriteSuccess(w, r, http.StatusOK, investigationWorkflowListResponse{
		SchemaVersion: "investigation-workflows.v1",
		Workflows:     workflows,
		Versions:      InvestigationWorkflowCatalogVersions(),
		Count:         len(page),
		Total:         total,
		Limit:         limit,
		Offset:        offset,
		Truncated:     truncated,
		NextOffset:    querycontract.NextOffset(offset, limit, truncated),
	}, h.truth("deterministic guided investigation workflow catalog; no live backend read"))
}

// pageInvestigationWorkflows applies offset and limit and reports whether
// more entries remain past the returned page.
func pageInvestigationWorkflows(catalog []InvestigationWorkflow, offset, limit int) ([]InvestigationWorkflow, bool) {
	if offset >= len(catalog) {
		return []InvestigationWorkflow{}, false
	}
	end := offset + limit
	truncated := end < len(catalog)
	if end > len(catalog) {
		end = len(catalog)
	}
	return catalog[offset:end], truncated
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
	if querycontract.AcceptsEnvelope(r) {
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
