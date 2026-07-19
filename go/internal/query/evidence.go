// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const relationshipEvidenceCapability = "relationship_evidence.drilldown"

// EvidenceHandler exposes drilldown endpoints for compact evidence pointers
// returned by graph and repository context surfaces.
type EvidenceHandler struct {
	Content            ContentStore
	AdmissionDecisions AdmissionDecisionReadStore
	Profile            QueryProfile
}

type relationshipEvidenceReadModel struct {
	Available bool
	Row       map[string]any
}

type relationshipEvidenceReadModelStore interface {
	relationshipEvidenceByResolvedID(context.Context, string) (relationshipEvidenceReadModel, error)
}

// Mount registers evidence drilldown routes.
func (h *EvidenceHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/evidence/relationships/{resolved_id}", h.getRelationshipEvidence)
	mux.HandleFunc("GET /api/v0/evidence/admission-decisions", h.listAdmissionDecisions)
	mux.HandleFunc("GET /api/v0/investigations/deployable-unit/packet", h.getDeployableUnitPacket)
	mux.HandleFunc("POST /api/v0/evidence/citations", h.buildEvidenceCitations)
}

func (h *EvidenceHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

// relationshipEvidenceRowWithinAccess reports whether the resolved
// relationship row's source and target endpoints are both attributable to
// access's granted repositories/ingestion scopes. Shared/admin/local callers
// (access.scoped() == false) always pass. A scoped caller with no grants, or
// whose row carries an empty or out-of-grant repo_id on either endpoint,
// fails closed (see the #5167 doc comment at the call site).
func relationshipEvidenceRowWithinAccess(row map[string]any, access repositoryAccessFilter) bool {
	if !access.scoped() {
		return true
	}
	if access.empty() {
		return false
	}
	return access.allowsRepositoryID(relationshipEvidenceEndpointRepoID(row, "source")) &&
		access.allowsRepositoryID(relationshipEvidenceEndpointRepoID(row, "target"))
}

// relationshipEvidenceEndpointRepoID reads the repo_id string off the
// "source" or "target" endpoint map relationshipEvidenceEndpoint built.
func relationshipEvidenceEndpointRepoID(row map[string]any, key string) string {
	endpoint, _ := row[key].(map[string]any)
	if endpoint == nil {
		return ""
	}
	return StringVal(endpoint, "repo_id")
}

func (h *EvidenceHandler) getRelationshipEvidence(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryRelationshipEvidence,
		"GET /api/v0/evidence/relationships/{resolved_id}",
		relationshipEvidenceCapability,
	)
	defer span.End()

	resolvedID := strings.TrimSpace(PathParam(r, "resolved_id"))
	if resolvedID == "" {
		WriteError(w, http.StatusBadRequest, "resolved_id is required")
		return
	}
	if h.Content == nil {
		WriteError(w, http.StatusNotImplemented, "relationship evidence drilldown requires the Postgres relationship read model")
		return
	}
	store, ok := h.Content.(relationshipEvidenceReadModelStore)
	if !ok {
		WriteError(w, http.StatusNotImplemented, "relationship evidence drilldown requires the Postgres relationship read model")
		return
	}
	readModel, err := store.relationshipEvidenceByResolvedID(r.Context(), resolvedID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !readModel.Available || len(readModel.Row) == 0 {
		WriteError(w, http.StatusNotFound, "relationship evidence not found")
		return
	}
	// Access scoping (#5167 Group B): the resolved relationship connects a
	// source and target endpoint, each carrying its own repo_id
	// (relationshipEvidenceEndpoint in evidence_read_model.go). A scoped
	// caller may only see the edge when BOTH endpoints are attributable to a
	// granted repository or ingestion scope -- mirroring
	// scopedInfraRelationshipsRoute's "both endpoints" contract
	// (infra_scope.go) -- otherwise the relationship is served as not-found,
	// disclosing neither the edge's existence nor either endpoint's identity
	// to an out-of-grant caller.
	access := repositoryAccessFilterFromContext(r.Context())
	if !relationshipEvidenceRowWithinAccess(readModel.Row, access) {
		WriteError(w, http.StatusNotFound, "relationship evidence not found")
		return
	}
	addRelationshipConfidenceBasis(readModel.Row)
	WriteSuccess(w, r, http.StatusOK, readModel.Row, BuildTruthEnvelope(
		h.profile(),
		relationshipEvidenceCapability,
		TruthBasisSemanticFacts,
		"resolved from durable Postgres relationship evidence by resolved_id",
	))
}
