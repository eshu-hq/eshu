package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	sbomAttestationAttachmentsCapability = "supply_chain.sbom_attestation_attachments.list"
	sbomAttestationAttachmentMaxLimit    = 200
)

// SupplyChainHandler exposes reducer-owned supply-chain read models.
type SupplyChainHandler struct {
	SBOMAttachments SBOMAttestationAttachmentStore
	Profile         QueryProfile
}

// SBOMAttestationAttachmentResult is one reducer-owned SBOM or attestation
// attachment row returned by the public API.
type SBOMAttestationAttachmentResult struct {
	AttachmentID       string                 `json:"attachment_id"`
	SubjectDigest      string                 `json:"subject_digest,omitempty"`
	DocumentID         string                 `json:"document_id,omitempty"`
	DocumentDigest     string                 `json:"document_digest,omitempty"`
	AttachmentStatus   string                 `json:"attachment_status"`
	ParseStatus        string                 `json:"parse_status,omitempty"`
	VerificationStatus string                 `json:"verification_status,omitempty"`
	VerificationPolicy string                 `json:"verification_policy,omitempty"`
	ArtifactKind       string                 `json:"artifact_kind,omitempty"`
	Format             string                 `json:"format,omitempty"`
	SpecVersion        string                 `json:"spec_version,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	CanonicalWrites    int                    `json:"canonical_writes"`
	ComponentCount     int                    `json:"component_count"`
	ComponentEvidence  []ComponentEvidenceRow `json:"component_evidence,omitempty"`
	WarningSummaries   []string               `json:"warning_summaries,omitempty"`
	EvidenceFactIDs    []string               `json:"evidence_fact_ids,omitempty"`
	SourceFreshness    string                 `json:"source_freshness,omitempty"`
	SourceConfidence   string                 `json:"source_confidence,omitempty"`
}

// Mount registers supply-chain query routes.
func (h *SupplyChainHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments", h.listSBOMAttachments)
}

func (h *SupplyChainHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *SupplyChainHandler) listSBOMAttachments(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySBOMAttestationAttachments,
		"GET /api/v0/supply-chain/sbom-attestations/attachments",
		sbomAttestationAttachmentsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), sbomAttestationAttachmentsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"SBOM and attestation attachments require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			sbomAttestationAttachmentsCapability,
			h.profile(),
			requiredProfile(sbomAttestationAttachmentsCapability),
		)
		return
	}
	limit, ok := requiredSBOMAttestationAttachmentLimit(w, r)
	if !ok {
		return
	}
	filter := SBOMAttestationAttachmentFilter{
		SubjectDigest:     QueryParam(r, "subject_digest"),
		DocumentID:        QueryParam(r, "document_id"),
		DocumentDigest:    QueryParam(r, "document_digest"),
		AttachmentStatus:  QueryParam(r, "attachment_status"),
		ArtifactKind:      QueryParam(r, "artifact_kind"),
		AfterAttachmentID: QueryParam(r, "after_attachment_id"),
		Limit:             limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "subject_digest, document_id, or document_digest is required")
		return
	}
	if h.SBOMAttachments == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"SBOM and attestation attachments require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			sbomAttestationAttachmentsCapability,
			h.profile(),
			requiredProfile(sbomAttestationAttachmentsCapability),
		)
		return
	}

	rows, err := h.SBOMAttachments.ListSBOMAttestationAttachments(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SBOMAttestationAttachmentResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SBOMAttestationAttachmentResult(row))
	}
	body := map[string]any{
		"attachments": results,
		"count":       len(results),
		"limit":       limit,
		"truncated":   truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_attachment_id": results[len(results)-1].AttachmentID,
		}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		sbomAttestationAttachmentsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned SBOM and attestation attachment facts; parse validity and verification trust remain separate",
	))
}

func requiredSBOMAttestationAttachmentLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > sbomAttestationAttachmentMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", sbomAttestationAttachmentMaxLimit))
		return 0, false
	}
	return limit, true
}
