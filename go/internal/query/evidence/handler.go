// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

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

// Handler exposes drilldown endpoints for compact evidence pointers
// returned by graph and repository context surfaces. Root package query
// keeps the pre-move spelling through its EvidenceHandler alias (see
// compat_evidence.go); cmd wiring constructs this struct with the same
// keyed fields.
type Handler struct {
	Content            querycontract.ContentStore
	AdmissionDecisions AdmissionDecisionReadStore
	Profile            querycontract.QueryProfile
	// StatusReader and Neo4j back GET /api/v0/evidence/bundle (#4045): the
	// live evidence-bundle composer reads the same status snapshot and
	// repository count StatusHandler's getPipelineStatus, getIndexStatus, and
	// listCollectors already read (see bundle_live.go). A nil
	// StatusReader is treated as "not configured" (503), matching the staying
	// status-backed routes in root package query.
	StatusReader status.Reader
	Neo4j        GraphQuery
	// PacketResponder composes and writes the deployable-unit investigation
	// packet. Root package query provides it from the lane-B packet
	// envelope; cmd wiring always injects it. A nil responder answers the
	// packet route with 503 rather than composing without the envelope.
	PacketResponder DeployableUnitPacketResponder
}

// RelationshipEvidenceReadModel is the shared read model: the durable
// Postgres relationship evidence row behind GET
// /api/v0/evidence/relationships/{resolved_id}. The ContentReader in root
// returns it and the test doubles outside this package name it (#6060).
type RelationshipEvidenceReadModel = repository.RelationshipEvidenceReadModel

// RelationshipEvidenceReadModelStore is the narrow port the relationship
// evidence drilldown asserts its content store through. Root's ContentReader
// satisfies it; a store without the read model answers 501. See the
// build-time assertion block in root's content_reader.go.
type RelationshipEvidenceReadModelStore interface {
	RelationshipEvidenceByResolvedID(context.Context, string) (RelationshipEvidenceReadModel, error)
}

// Mount registers evidence drilldown routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/evidence/relationships/{resolved_id}", h.getRelationshipEvidence)
	mux.HandleFunc("GET /api/v0/evidence/admission-decisions", h.listAdmissionDecisions)
	mux.HandleFunc("GET /api/v0/evidence/bundle", h.getLiveEvidenceBundle)
	mux.HandleFunc("GET /api/v0/investigations/deployable-unit/packet", h.getDeployableUnitPacket)
	mux.HandleFunc("POST /api/v0/evidence/citations", h.buildEvidenceCitations)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) getRelationshipEvidence(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryRelationshipEvidence,
		"GET /api/v0/evidence/relationships/{resolved_id}",
		relationshipEvidenceCapability,
	)
	defer span.End()

	resolvedID := strings.TrimSpace(querycontract.PathParam(r, "resolved_id"))
	if resolvedID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "resolved_id is required")
		return
	}
	if h.Content == nil {
		querycontract.WriteError(w, http.StatusNotImplemented, "relationship evidence drilldown requires the Postgres relationship read model")
		return
	}
	store, ok := h.Content.(RelationshipEvidenceReadModelStore)
	if !ok {
		querycontract.WriteError(w, http.StatusNotImplemented, "relationship evidence drilldown requires the Postgres relationship read model")
		return
	}
	readModel, err := store.RelationshipEvidenceByResolvedID(r.Context(), resolvedID)
	if err != nil {
		writeReadFailure(w, r, err, relationshipEvidenceCapability, relationshipEvidenceReadFailedMessage)
		return
	}
	if !readModel.Available || len(readModel.Row) == 0 {
		querycontract.WriteError(w, http.StatusNotFound, "relationship evidence not found")
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
	if !querycontract.RelationshipEvidenceRowWithinAccess(readModel.Row, access) {
		querycontract.WriteError(w, http.StatusNotFound, "relationship evidence not found")
		return
	}
	querycontract.AddRelationshipConfidenceBasis(readModel.Row)
	querycontract.WriteSuccess(w, r, http.StatusOK, readModel.Row, querycontract.BuildTruthEnvelope(
		h.profile(),
		relationshipEvidenceCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from durable Postgres relationship evidence by resolved_id",
	))
}
