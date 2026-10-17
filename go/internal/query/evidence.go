// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6642 evidence slice: EvidenceHandler stays in root until the querycontract evidence-authz pre-slice (the blocking dependency) and then the full evidence leaf move carry it; see query/evidence/doc.go.

import (
	"context"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// relationshipEvidenceReadFailedMessage is the fixed body of a failed read on
// GET /api/v0/evidence/relationships/{resolved_id} (#7674). The store error
// can quote SQL and hosts, so it goes on the request span, never into the
// body.
const relationshipEvidenceReadFailedMessage = "relationship evidence read failed"

const relationshipEvidenceCapability = "relationship_evidence.drilldown"

// EvidenceHandler exposes drilldown endpoints for compact evidence pointers
// returned by graph and repository context surfaces.
type EvidenceHandler struct {
	Content            ContentStore
	AdmissionDecisions AdmissionDecisionReadStore
	Profile            QueryProfile
	// StatusReader and Neo4j back GET /api/v0/evidence/bundle (#4045): the
	// live evidence-bundle composer reads the same status snapshot and
	// repository count StatusHandler's getPipelineStatus, getIndexStatus, and
	// listCollectors already read (see evidence_bundle_live.go). A nil
	// StatusReader is treated as "not configured" (503), matching every other
	// status-backed route in this package.
	StatusReader status.Reader
	Neo4j        GraphQuery
}

// relationshipEvidenceReadModel is the shared read model, aliased so this
// package's call sites keep their unexported spelling while a ContentStore
// double outside package query can still name it (#6060).
type relationshipEvidenceReadModel = repository.RelationshipEvidenceReadModel

type relationshipEvidenceReadModelStore interface {
	RelationshipEvidenceByResolvedID(context.Context, string) (relationshipEvidenceReadModel, error)
}

// Mount registers evidence drilldown routes.
func (h *EvidenceHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/evidence/relationships/{resolved_id}", h.getRelationshipEvidence)
	mux.HandleFunc("GET /api/v0/evidence/admission-decisions", h.listAdmissionDecisions)
	mux.HandleFunc("GET /api/v0/evidence/bundle", h.getLiveEvidenceBundle)
	mux.HandleFunc("GET /api/v0/investigations/deployable-unit/packet", h.getDeployableUnitPacket)
	mux.HandleFunc("POST /api/v0/evidence/citations", h.buildEvidenceCitations)
}

func (h *EvidenceHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

// relationshipEvidenceRowWithinAccess is the root forwarder for the
// relationship-evidence access gate (#6060 pattern): the handler and the
// scoped tests in this package keep the unexported spelling, while the
// predicate itself -- the verb→targetAttributable classification, the
// endpoint-repo reader, and the both-endpoints gate over them -- lives in
// querycontract (relationship_evidence_authz.go, the #6642 evidence-leaf
// pre-slice) so the evidence leaf can share the single source of truth
// without importing the root query package.
func relationshipEvidenceRowWithinAccess(row map[string]any, access querycontract.RepositoryAccessFilter) bool {
	return querycontract.RelationshipEvidenceRowWithinAccess(row, access)
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
	readModel, err := store.RelationshipEvidenceByResolvedID(r.Context(), resolvedID)
	if err != nil {
		writeReadFailure(w, r, err, relationshipEvidenceCapability, relationshipEvidenceReadFailedMessage)
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
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
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
