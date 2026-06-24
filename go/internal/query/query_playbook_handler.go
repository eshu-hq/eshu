// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// QueryPlaybookHandler exposes the deterministic playbook catalog and resolver
// through the HTTP API. It reads only in-process catalog data and never calls
// Postgres, graph backends, or external providers.
type QueryPlaybookHandler struct {
	Profile QueryProfile
}

// Mount registers query playbook catalog and resolver routes.
func (h *QueryPlaybookHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/query-playbooks", h.list)
	mux.HandleFunc("POST /api/v0/query-playbooks/resolve", h.resolve)
}

type queryPlaybookListResponse struct {
	SchemaVersion string               `json:"schema_version"`
	Playbooks     []QueryPlaybook      `json:"playbooks"`
	Versions      []PlaybookVersionRef `json:"versions"`
	Count         int                  `json:"count"`
}

type queryPlaybookResolveRequest struct {
	PlaybookID string            `json:"playbook_id"`
	Inputs     map[string]string `json:"inputs"`
}

type queryPlaybookResolveResponse struct {
	SchemaVersion string           `json:"schema_version"`
	Resolved      ResolvedPlaybook `json:"resolved"`
}

func (h *QueryPlaybookHandler) list(w http.ResponseWriter, r *http.Request) {
	catalog := PlaybookCatalog()
	WriteSuccess(w, r, http.StatusOK, queryPlaybookListResponse{
		SchemaVersion: "query-playbooks.v1",
		Playbooks:     catalog,
		Versions:      PlaybookCatalogVersions(),
		Count:         len(catalog),
	}, h.truth("deterministic query playbook catalog; no live backend read"))
}

func (h *QueryPlaybookHandler) resolve(w http.ResponseWriter, r *http.Request) {
	var req queryPlaybookResolveRequest
	if err := ReadJSON(r, &req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "invalid query playbook resolve request")
		return
	}
	playbookID := strings.TrimSpace(req.PlaybookID)
	if playbookID == "" {
		h.writeError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "playbook_id is required")
		return
	}
	playbook, ok := LookupPlaybook(playbookID)
	if !ok {
		h.writeError(w, r, http.StatusNotFound, ErrorCodeNotFound, "query playbook not found")
		return
	}
	resolved, err := playbook.Resolve(req.Inputs)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, queryPlaybookResolveResponse{
		SchemaVersion: "query-playbooks.v1",
		Resolved:      resolved,
	}, h.truth("deterministic query playbook resolver; no live backend read"))
}

func (h *QueryPlaybookHandler) truth(reason string) *TruthEnvelope {
	return BuildTruthEnvelope(h.profile(), CapabilityQueryPlaybooks, TruthBasisRuntimeState, reason)
}

func (h *QueryPlaybookHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data: nil,
			Error: &ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: CapabilityQueryPlaybooks,
			},
		})
		return
	}
	WriteError(w, status, message)
}

func (h *QueryPlaybookHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}
