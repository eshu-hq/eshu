// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SBOMAttestationAttachmentResult is one reducer-owned SBOM or attestation
// attachment row returned by the public API.
type SBOMAttestationAttachmentResult struct {
	AttachmentID               string                 `json:"attachment_id"`
	SubjectDigest              string                 `json:"subject_digest,omitempty"`
	DocumentID                 string                 `json:"document_id,omitempty"`
	DocumentDigest             string                 `json:"document_digest,omitempty"`
	AttachmentStatus           string                 `json:"attachment_status"`
	ParseStatus                string                 `json:"parse_status,omitempty"`
	VerificationStatus         string                 `json:"verification_status,omitempty"`
	VerificationPolicy         string                 `json:"verification_policy,omitempty"`
	ArtifactKind               string                 `json:"artifact_kind,omitempty"`
	Format                     string                 `json:"format,omitempty"`
	SpecVersion                string                 `json:"spec_version,omitempty"`
	Reason                     string                 `json:"reason,omitempty"`
	AttachmentScope            string                 `json:"attachment_scope,omitempty"`
	CanonicalWrites            int                    `json:"canonical_writes"`
	ComponentCount             int                    `json:"component_count"`
	ComponentEvidence          []ComponentEvidenceRow `json:"component_evidence,omitempty"`
	ComponentEvidenceTruncated bool                   `json:"component_evidence_truncated"`
	// DependencyRelationships, DependencyRelationshipCount, and
	// DependencyRelationshipsTruncated mirror ComponentEvidence's evidence
	// contract for sbom.dependency_relationship evidence: bounded rows plus
	// an honest full count and truncation flag. See
	// SBOMAttestationAttachmentRow for the shared field documentation.
	DependencyRelationships          []DependencyRelationshipRow `json:"dependency_relationships,omitempty"`
	DependencyRelationshipCount      int                         `json:"dependency_relationship_count"`
	DependencyRelationshipsTruncated bool                        `json:"dependency_relationships_truncated"`
	// ExternalReferences mirrors DependencyRelationships for
	// sbom.external_reference evidence.
	ExternalReferences          []ExternalReferenceRow `json:"external_references,omitempty"`
	ExternalReferenceCount      int                    `json:"external_reference_count"`
	ExternalReferencesTruncated bool                   `json:"external_references_truncated"`
	// SLSAProvenancePredicateType and SLSAProvenanceBuilderID mirror
	// SBOMAttestationAttachmentRow's fields of the same name: the joined
	// attestation.slsa_provenance evidence for this statement, empty when no
	// such fact joined.
	SLSAProvenancePredicateType string `json:"slsa_provenance_predicate_type,omitempty"`
	SLSAProvenanceBuilderID     string `json:"slsa_provenance_builder_id,omitempty"`
	// SLSAProvenanceMaterials, SLSAProvenanceMaterialCount, and
	// SLSAProvenanceMaterialsTruncated (#5456) mirror
	// SBOMAttestationAttachmentRow's fields of the same name — see there for
	// the shared field documentation. Field name, type, and ORDER must match
	// SBOMAttestationAttachmentRow exactly: buildSBOMAttestationAttachmentResult
	// does a raw struct conversion between the two.
	SLSAProvenanceMaterials              []SLSAMaterialRow `json:"slsa_provenance_materials,omitempty"`
	SLSAProvenanceMaterialCount          int               `json:"slsa_provenance_material_count"`
	SLSAProvenanceMaterialsTruncated     bool              `json:"slsa_provenance_materials_truncated"`
	SLSAProvenanceConfigSourceURI        string            `json:"slsa_provenance_config_source_uri,omitempty"`
	SLSAProvenanceConfigSourceEntryPoint string            `json:"slsa_provenance_config_source_entry_point,omitempty"`
	SLSAProvenanceConfigSourceDigest     map[string]string `json:"slsa_provenance_config_source_digest,omitempty"`
	RepositoryIDs                        []string          `json:"repository_ids,omitempty"`
	WorkloadIDs                          []string          `json:"workload_ids,omitempty"`
	ServiceIDs                           []string          `json:"service_ids,omitempty"`
	WarningSummaries                     []string          `json:"warning_summaries,omitempty"`
	WarningSummaryCount                  int               `json:"warning_summary_count"`
	WarningSummariesTruncated            bool              `json:"warning_summaries_truncated"`
	EvidenceFactIDs                      []string          `json:"evidence_fact_ids,omitempty"`
	MissingEvidence                      []string          `json:"missing_evidence,omitempty"`
	SourceFreshness                      string            `json:"source_freshness,omitempty"`
	SourceConfidence                     string            `json:"source_confidence,omitempty"`
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
	// Empty scoped grants return the zero-attachments page without resolving a
	// selector or reading the attachment store.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptySBOMAttachmentPage(w, r, limit)
		return
	}
	repositoryID, ok := h.resolveSBOMAttachmentRepositorySelector(w, r, QueryParam(r, "repository_id"), access, sbomAttestationAttachmentsCapability)
	if !ok {
		return
	}
	filter := SBOMAttestationAttachmentFilter{
		SubjectDigest:              firstNonEmptyQueryParam(r, "subject_digest", "digest"),
		DocumentID:                 QueryParam(r, "document_id"),
		DocumentDigest:             QueryParam(r, "document_digest"),
		RepositoryID:               repositoryID,
		WorkloadID:                 QueryParam(r, "workload_id"),
		ServiceID:                  QueryParam(r, "service_id"),
		AttachmentStatus:           QueryParam(r, "attachment_status"),
		ArtifactKind:               QueryParam(r, "artifact_kind"),
		AfterAttachmentID:          QueryParam(r, "after_attachment_id"),
		Limit:                      limit + 1,
		AllowedSourceRepositoryIDs: access.repositorySearchIDs(),
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "subject_digest, document_id, document_digest, repository_id, workload_id, or service_id is required")
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

	page, err := h.SBOMAttachments.ListSBOMAttestationAttachments(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := page.Attachments
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SBOMAttestationAttachmentResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, buildSBOMAttestationAttachmentResult(row))
	}
	body := map[string]any{
		"attachments": results,
		"count":       len(results),
		"limit":       limit,
		"truncated":   truncated,
	}
	if len(page.MissingEvidence) > 0 {
		body["missing_evidence"] = page.MissingEvidence
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_attachment_id": results[len(results)-1].AttachmentID,
		}
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorSBOMAttestation, len(results), truncated)
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
