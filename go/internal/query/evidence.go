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
	Content ContentStore
	Profile QueryProfile
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
}

func (h *EvidenceHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
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
	WriteSuccess(w, r, http.StatusOK, readModel.Row, BuildTruthEnvelope(
		h.profile(),
		relationshipEvidenceCapability,
		TruthBasisSemanticFacts,
		"resolved from durable Postgres relationship evidence by resolved_id",
	))
}
