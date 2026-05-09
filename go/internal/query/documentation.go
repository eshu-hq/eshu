package query

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	UpdatedSince   *time.Time
	Limit          int
	Cursor         string
	Offset         int
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
	documentationEvidencePacketFreshness(context.Context, string, string) (documentationEvidencePacketFreshnessReadModel, error)
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
	updatedSince, ok := documentationUpdatedSince(w, r)
	if !ok {
		return
	}
	page, ok := documentationPagination(w, r)
	if !ok {
		return
	}
	store, ok := h.documentationStore(w, r)
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
		UpdatedSince:   updatedSince,
		Limit:          page.limit,
		Cursor:         page.cursor,
		Offset:         page.offset,
	})
	if err != nil {
		writeDocumentationInternalError(w, r)
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
		writeDocumentationError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "finding_id is required", "")
		return
	}
	store, ok := h.documentationStore(w, r)
	if !ok {
		return
	}
	readModel, err := store.documentationEvidencePacket(r.Context(), findingID)
	if err != nil {
		writeDocumentationInternalError(w, r)
		return
	}
	if readModel.Denied {
		writeDocumentationPermissionDenied(w, r, readModel.DeniedReason)
		return
	}
	if !readModel.Available || len(readModel.Packet) == 0 {
		writeDocumentationError(
			w,
			r,
			http.StatusNotFound,
			ErrorCodeNotFound,
			"documentation evidence packet not found",
			"",
		)
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
		writeDocumentationError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "packet_id is required", "")
		return
	}
	packetVersion := QueryParam(r, "packet_version")
	store, ok := h.documentationStore(w, r)
	if !ok {
		return
	}
	readModel, err := store.documentationEvidencePacketFreshness(r.Context(), packetID, packetVersion)
	if err != nil {
		writeDocumentationInternalError(w, r)
		return
	}
	if readModel.Denied {
		writeDocumentationPermissionDenied(w, r, readModel.DeniedReason)
		return
	}
	if !readModel.Available {
		writeDocumentationError(
			w,
			r,
			http.StatusNotFound,
			ErrorCodeNotFound,
			"documentation evidence packet not found",
			"",
		)
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
		writeDocumentationCapabilityError(
			w,
			r,
			http.StatusNotImplemented,
			ErrorCodeUnsupportedCapability,
			"documentation evidence packets require durable documentation facts",
			capability,
			h.profile(),
		)
		return true
	}
	return false
}

func (h *DocumentationHandler) documentationStore(
	w http.ResponseWriter,
	r *http.Request,
) (documentationReadModelStore, bool) {
	if h.Content == nil {
		writeDocumentationError(
			w,
			r,
			http.StatusNotImplemented,
			ErrorCodeReadModelUnavailable,
			"documentation evidence packets require the Postgres documentation read model",
			"",
		)
		return nil, false
	}
	store, ok := h.Content.(documentationReadModelStore)
	if !ok {
		writeDocumentationError(
			w,
			r,
			http.StatusNotImplemented,
			ErrorCodeReadModelUnavailable,
			"documentation evidence packets require the Postgres documentation read model",
			"",
		)
		return nil, false
	}
	return store, true
}

type documentationPage struct {
	limit  int
	cursor string
	offset int
}

func documentationPagination(w http.ResponseWriter, r *http.Request) (documentationPage, bool) {
	page := documentationPage{limit: 50}
	rawLimit := QueryParam(r, "limit")
	if rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > 200 {
			writeDocumentationError(
				w,
				r,
				http.StatusBadRequest,
				ErrorCodeInvalidArgument,
				"limit must be an integer between 1 and 200",
				"",
			)
			return documentationPage{}, false
		}
		page.limit = limit
	}
	page.cursor = QueryParam(r, "cursor")
	if page.cursor != "" {
		offset, err := strconv.Atoi(page.cursor)
		if err != nil || offset < 0 {
			writeDocumentationError(
				w,
				r,
				http.StatusBadRequest,
				ErrorCodeInvalidArgument,
				"cursor must be a non-negative integer offset",
				"",
			)
			return documentationPage{}, false
		}
		page.offset = offset
	}
	return page, true
}

func documentationUpdatedSince(w http.ResponseWriter, r *http.Request) (*time.Time, bool) {
	raw := QueryParam(r, "updated_since")
	if raw == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeDocumentationError(
			w,
			r,
			http.StatusBadRequest,
			ErrorCodeInvalidArgument,
			"updated_since must be an RFC3339 timestamp",
			"",
		)
		return nil, false
	}
	return &parsed, true
}

func writeDocumentationInternalError(w http.ResponseWriter, r *http.Request) {
	writeDocumentationError(
		w,
		r,
		http.StatusInternalServerError,
		ErrorCodeInternalError,
		"documentation evidence request failed",
		"",
	)
}

func writeDocumentationPermissionDenied(w http.ResponseWriter, r *http.Request, reason string) {
	if strings.TrimSpace(reason) == "" {
		reason = "caller cannot view documentation evidence"
	}
	writeDocumentationError(w, r, http.StatusForbidden, ErrorCodePermissionDenied, reason, "")
}

func writeDocumentationError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code ErrorCode,
	message string,
	capability string,
) {
	correlationID := documentationCorrelationID(r)
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:          code,
			Message:       message,
			Capability:    capability,
			CorrelationID: correlationID,
		}})
		return
	}
	body := map[string]any{
		"error_code":     code,
		"message":        message,
		"correlation_id": correlationID,
	}
	if capability != "" {
		body["capability"] = capability
	}
	WriteJSON(w, status, body)
}

func writeDocumentationCapabilityError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code ErrorCode,
	message string,
	capability string,
	currentProfile QueryProfile,
) {
	correlationID := documentationCorrelationID(r)
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:          code,
			Message:       message,
			Capability:    capability,
			CorrelationID: correlationID,
			Profiles: &ErrorProfiles{
				Current:  currentProfile,
				Required: requiredProfile(capability),
			},
		}})
		return
	}
	body := map[string]any{
		"error_code":     code,
		"message":        message,
		"capability":     capability,
		"correlation_id": correlationID,
	}
	WriteJSON(w, status, body)
}

func documentationCorrelationID(r *http.Request) string {
	for _, header := range []string{"X-Correlation-ID", "X-Request-ID"} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	}
	return hex.EncodeToString(raw[:])
}
