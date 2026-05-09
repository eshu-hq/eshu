package query

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	documentationFindingsCapability        = "documentation_findings.list"
	documentationEvidencePacketCapability  = "documentation_evidence_packet.read"
	documentationPacketFreshnessCapability = "documentation_evidence_packet.freshness"
)

// DocumentationHandler exposes documentation truth findings and evidence packets.
type DocumentationHandler struct {
	Content ContentStore
	Profile QueryProfile
}

type documentationFindingFilter struct {
	FindingType    string
	SourceID       string
	DocumentID     string
	Status         string
	TruthLevel     string
	FreshnessState string
	UpdatedSince   string
	Limit          int
	Cursor         string
}

type documentationFindingListReadModel struct {
	Findings   []map[string]any
	NextCursor string
}

type documentationEvidencePacketReadModel struct {
	Available    bool
	Denied       bool
	DeniedReason string
	Packet       map[string]any
}

type documentationEvidencePacketFreshnessReadModel struct {
	Available           bool
	Denied              bool
	DeniedReason        string
	PacketID            string `json:"packet_id"`
	PacketVersion       string `json:"packet_version"`
	FreshnessState      string `json:"freshness_state"`
	LatestPacketVersion string `json:"latest_packet_version"`
}

type documentationReadModelStore interface {
	documentationFindings(context.Context, documentationFindingFilter) (documentationFindingListReadModel, error)
	documentationEvidencePacket(context.Context, string) (documentationEvidencePacketReadModel, error)
	documentationEvidencePacketFreshness(context.Context, string) (documentationEvidencePacketFreshnessReadModel, error)
}

// Mount registers documentation truth routes.
func (h *DocumentationHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/documentation/findings", h.listFindings)
	mux.HandleFunc("GET /api/v0/documentation/findings/{finding_id}/evidence-packet", h.getEvidencePacket)
	mux.HandleFunc("GET /api/v0/documentation/evidence-packets/{packet_id}/freshness", h.getPacketFreshness)
}

func (h *DocumentationHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *DocumentationHandler) listFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDocumentationFindings,
		"GET /api/v0/documentation/findings",
		documentationFindingsCapability,
	)
	defer span.End()

	if h.unsupported(w, r, documentationFindingsCapability) {
		return
	}
	store, ok := h.documentationStore(w)
	if !ok {
		return
	}
	readModel, err := store.documentationFindings(r.Context(), documentationFindingFilter{
		FindingType:    QueryParam(r, "finding_type"),
		SourceID:       QueryParam(r, "source_id"),
		DocumentID:     QueryParam(r, "document_id"),
		Status:         QueryParam(r, "status"),
		TruthLevel:     QueryParam(r, "truth_level"),
		FreshnessState: QueryParam(r, "freshness_state"),
		UpdatedSince:   QueryParam(r, "updated_since"),
		Limit:          documentationLimit(r),
		Cursor:         QueryParam(r, "cursor"),
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"findings":    readModel.Findings,
		"next_cursor": readModel.NextCursor,
	}, BuildTruthEnvelope(
		h.profile(),
		documentationFindingsCapability,
		TruthBasisSemanticFacts,
		"resolved from durable documentation finding facts",
	))
}

func (h *DocumentationHandler) getEvidencePacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDocumentationEvidencePacket,
		"GET /api/v0/documentation/findings/{finding_id}/evidence-packet",
		documentationEvidencePacketCapability,
	)
	defer span.End()

	if h.unsupported(w, r, documentationEvidencePacketCapability) {
		return
	}
	findingID := strings.TrimSpace(PathParam(r, "finding_id"))
	if findingID == "" {
		WriteError(w, http.StatusBadRequest, "finding_id is required")
		return
	}
	store, ok := h.documentationStore(w)
	if !ok {
		return
	}
	readModel, err := store.documentationEvidencePacket(r.Context(), findingID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if readModel.Denied {
		writeDocumentationPermissionDenied(w, readModel.DeniedReason)
		return
	}
	if !readModel.Available || len(readModel.Packet) == 0 {
		WriteError(w, http.StatusNotFound, "documentation evidence packet not found")
		return
	}
	WriteSuccess(w, r, http.StatusOK, readModel.Packet, BuildTruthEnvelope(
		h.profile(),
		documentationEvidencePacketCapability,
		TruthBasisSemanticFacts,
		"resolved from durable documentation evidence packet facts",
	))
}

func (h *DocumentationHandler) getPacketFreshness(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDocumentationPacketFreshness,
		"GET /api/v0/documentation/evidence-packets/{packet_id}/freshness",
		documentationPacketFreshnessCapability,
	)
	defer span.End()

	if h.unsupported(w, r, documentationPacketFreshnessCapability) {
		return
	}
	packetID := strings.TrimSpace(PathParam(r, "packet_id"))
	if packetID == "" {
		WriteError(w, http.StatusBadRequest, "packet_id is required")
		return
	}
	store, ok := h.documentationStore(w)
	if !ok {
		return
	}
	readModel, err := store.documentationEvidencePacketFreshness(r.Context(), packetID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if readModel.Denied {
		writeDocumentationPermissionDenied(w, readModel.DeniedReason)
		return
	}
	if !readModel.Available {
		WriteError(w, http.StatusNotFound, "documentation evidence packet not found")
		return
	}
	WriteSuccess(w, r, http.StatusOK, readModel, BuildTruthEnvelope(
		h.profile(),
		documentationPacketFreshnessCapability,
		TruthBasisSemanticFacts,
		"resolved from durable documentation evidence packet facts",
	))
}

func (h *DocumentationHandler) unsupported(w http.ResponseWriter, r *http.Request, capability string) bool {
	if capabilityUnsupported(h.profile(), capability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"documentation evidence packets require durable documentation facts",
			ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			requiredProfile(capability),
		)
		return true
	}
	return false
}

func (h *DocumentationHandler) documentationStore(w http.ResponseWriter) (documentationReadModelStore, bool) {
	if h.Content == nil {
		WriteError(w, http.StatusNotImplemented, "documentation evidence packets require the Postgres documentation read model")
		return nil, false
	}
	store, ok := h.Content.(documentationReadModelStore)
	if !ok {
		WriteError(w, http.StatusNotImplemented, "documentation evidence packets require the Postgres documentation read model")
		return nil, false
	}
	return store, true
}

func documentationLimit(r *http.Request) int {
	limit := QueryParamInt(r, "limit", 50)
	if limit < 1 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func writeDocumentationPermissionDenied(w http.ResponseWriter, reason string) {
	if strings.TrimSpace(reason) == "" {
		reason = "caller cannot view documentation evidence"
	}
	WriteJSON(w, http.StatusForbidden, map[string]any{
		"error_code": "permission_denied",
		"message":    reason,
	})
}

func documentationCursorOffset(cursor string) int {
	offset, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}
