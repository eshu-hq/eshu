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
// relationship row is visible to access's granted repositories/ingestion
// scopes. Shared/admin/local callers (access.scoped() == false) always pass;
// a scoped caller with no grants fails closed.
//
// A scoped caller must always own the SOURCE repository (resolved_relationships
// always carries a source repo). The TARGET grant is required only when the
// target endpoint carries a tenant attribution, mirroring the
// relationships/edges targetAttributable model (relationships_catalog_cypher.go,
// relationshipVerbEntry.targetAttributable): for a targetAttributable:false
// verb the target is a shared/global entity (IMPORTS->Module, RUNS_ON->Platform,
// QUERIES_TABLE->SqlTable, INVOKES_CLOUD_ACTION->CloudAction) with no repo_id,
// so there is no cross-tenant secret to protect and source ownership alone
// authorizes the read. Requiring a grant on that empty repo_id (the pre-#5167
// F-6 W6 review behavior) spuriously 404'd evidence a caller fully owned via
// the source. For a targetAttributable:true verb the target names a real
// repository, so the target grant is enforced.
func relationshipEvidenceRowWithinAccess(row map[string]any, access repositoryAccessFilter) bool {
	if !access.scoped() {
		return true
	}
	if access.empty() {
		return false
	}
	if !access.allowsRepositoryID(relationshipEvidenceEndpointRepoID(row, "source")) {
		return false
	}
	if !relationshipEvidenceTargetAttributable(row) {
		return true
	}
	return access.allowsRepositoryID(relationshipEvidenceEndpointRepoID(row, "target"))
}

// relationshipEvidenceTargetAttributable reports whether the row's target
// endpoint carries a tenant attribution that a scoped grant must gate on. It
// resolves the row's relationship_type against the fixed relationships-catalog
// verb set (relationshipVerbByName); a known verb uses its targetAttributable
// classification. An unknown verb (outside the 16-verb catalog) falls back
// fail-closed to the row's own target repo_id: a non-empty target repo_id
// names a real repository that must be protected, an empty one has no tenant
// secret, so source ownership suffices.
func relationshipEvidenceTargetAttributable(row map[string]any) bool {
	verb := strings.ToUpper(strings.TrimSpace(StringVal(row, "relationship_type")))
	if entry, ok := relationshipVerbByName[verb]; ok {
		return entry.targetAttributable
	}
	return relationshipEvidenceEndpointRepoID(row, "target") != ""
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
