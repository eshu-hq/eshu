// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Capability identifies deterministic playbook catalog and resolver reads
// (#6642). Root's envelope_aliases.go keeps its own CapabilityQueryPlaybooks =
// "query.playbooks" literal, and the Part C leaf query/contract registers
// its twin into querycontract's shared capability registry from
// capability_matrix.go's init() (a leaf this move must not edit), for
// production and for
// go/internal/mcp/dispatch_query_playbooks_authz_test.go. The two literals
// must be kept byte-identical by hand until a follow-up lane threads them
// through one shared declaration, the way root's hardcoded-secret
// registration already calls querycontract.HardcodedSecretSupport;
// TestQueryPlaybookCapabilityLockstep
// (go/internal/query/capability_lockstep_query_playbook_test.go) guards the
// two support rows against drift meanwhile. See capabilities.go's Support
// and main_test.go's TestMain, which register this same capability for this
// package's own test binary (root's init never links here). See README.md's
// Move evidence.
const Capability = "query.playbooks"

// Handler exposes the deterministic playbook catalog and resolver
// through the HTTP API. It reads only in-process catalog data and never calls
// Postgres, graph backends, or external providers.
type Handler struct {
	Profile querycontract.QueryProfile
}

// Mount registers query playbook catalog and resolver routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/query-playbooks", h.list)
	mux.HandleFunc("POST /api/v0/query-playbooks/resolve", h.resolve)
}

type listResponse struct {
	SchemaVersion string       `json:"schema_version"`
	Playbooks     []Definition `json:"playbooks"`
	Versions      []VersionRef `json:"versions"`
	Count         int          `json:"count"`
}

type resolveRequest struct {
	PlaybookID string            `json:"playbook_id"`
	Inputs     map[string]string `json:"inputs"`
}

type resolveResponse struct {
	SchemaVersion string           `json:"schema_version"`
	Resolved      ResolvedPlaybook `json:"resolved"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	catalog := Catalog()
	querycontract.WriteSuccess(w, r, http.StatusOK, listResponse{
		SchemaVersion: "query-playbooks.v1",
		Playbooks:     catalog,
		Versions:      CatalogVersions(),
		Count:         len(catalog),
	}, h.truth("deterministic query playbook catalog; no live backend read"))
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	var req resolveRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		h.writeError(w, r, http.StatusBadRequest, querycontract.ErrorCodeInvalidArgument, "invalid query playbook resolve request")
		return
	}
	playbookID := strings.TrimSpace(req.PlaybookID)
	if playbookID == "" {
		h.writeError(w, r, http.StatusBadRequest, querycontract.ErrorCodeInvalidArgument, "playbook_id is required")
		return
	}
	playbook, ok := Lookup(playbookID)
	if !ok {
		h.writeError(w, r, http.StatusNotFound, querycontract.ErrorCodeNotFound, "query playbook not found")
		return
	}
	resolved, err := playbook.Resolve(req.Inputs)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, querycontract.ErrorCodeInvalidArgument, err.Error())
		return
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, resolveResponse{
		SchemaVersion: "query-playbooks.v1",
		Resolved:      resolved,
	}, h.truth("deterministic query playbook resolver; no live backend read"))
}

func (h *Handler) truth(reason string) *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(h.profile(), Capability, querycontract.TruthBasisRuntimeState, reason)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code querycontract.ErrorCode, message string) {
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteJSON(w, status, querycontract.ResponseEnvelope{
			Data: nil,
			Error: &querycontract.ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: Capability,
			},
		})
		return
	}
	querycontract.WriteError(w, status, message)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}
