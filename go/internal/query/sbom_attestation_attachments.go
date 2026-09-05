// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// TODO(#4795 W2b / #4784 ADR): reducer_sbom_attestation_attachment is a
// GOVERNED reducer-derived kind per the #4784 ADR
// (docs/internal/design/4784-reducer-derived-fact-governance.md) — full
// governance requires a landed sdk/go/factschema struct, generated JSON
// Schema, and a typed reducer writer (producer:
// go/internal/reducer/sbom_attestation_attachment_writer.go) before
// decodeSBOMAttestationAttachmentRow below (and the
// sbomAttestationAttachmentMissingEvidenceQuery's read of the sibling
// reducer_container_image_identity kind, also governed and also unstructed)
// can move off raw payload/SQL predicate decode onto a typed factschema seam.
// No W1 issue is assigned for either kind yet; this file stays on the
// pre-existing raw path until that struct work lands.
const (
	sbomAttestationAttachmentFactKind = "reducer_sbom_attestation_attachment"
	// sbomAttestationWarningSummaryPreviewMaxCount moved to
	// internal/query/supplychain with BoundedSBOMWarningSummaries (#6060
	// lane A); root's forward in supply_chain_hub_alias.go keeps this
	// file's decode path on the same bound.
)

type sbomAttestationAttachmentQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresSBOMAttestationAttachmentStore reads active SBOM and attestation
// attachment facts from Postgres using bounded payload predicates.
type PostgresSBOMAttestationAttachmentStore struct {
	DB sbomAttestationAttachmentQueryer
}

// NewPostgresSBOMAttestationAttachmentStore creates the Postgres-backed SBOM
// and attestation attachment read model.
func NewPostgresSBOMAttestationAttachmentStore(
	db sbomAttestationAttachmentQueryer,
) PostgresSBOMAttestationAttachmentStore {
	return PostgresSBOMAttestationAttachmentStore{DB: db}
}

// ListSBOMAttestationAttachments returns one bounded page of active reducer
// attachment facts.
func (s PostgresSBOMAttestationAttachmentStore) ListSBOMAttestationAttachments(
	ctx context.Context,
	filter SBOMAttestationAttachmentFilter,
) (SBOMAttestationAttachmentPage, error) {
	if s.DB == nil {
		return SBOMAttestationAttachmentPage{}, fmt.Errorf("sbom attestation attachment database is required")
	}
	if !filter.HasScope() {
		return SBOMAttestationAttachmentPage{}, fmt.Errorf("subject_digest, document_id, document_digest, repository_id, workload_id, or service_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > sbomAttestationAttachmentMaxLimit+1 {
		return SBOMAttestationAttachmentPage{}, fmt.Errorf("limit must be between 1 and %d", sbomAttestationAttachmentMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listSBOMAttestationAttachmentsQuery,
		sbomAttestationAttachmentFactKind,
		filter.SubjectDigest,
		filter.DocumentID,
		filter.DocumentDigest,
		filter.AttachmentStatus,
		filter.ArtifactKind,
		filter.RepositoryID,
		filter.WorkloadID,
		filter.ServiceID,
		filter.AfterAttachmentID,
		filter.Limit,
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
	)
	if err != nil {
		return SBOMAttestationAttachmentPage{}, fmt.Errorf("list sbom attestation attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SBOMAttestationAttachmentRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return SBOMAttestationAttachmentPage{}, fmt.Errorf("list sbom attestation attachments: %w", err)
		}
		row, err := decodeSBOMAttestationAttachmentRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return SBOMAttestationAttachmentPage{}, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return SBOMAttestationAttachmentPage{}, fmt.Errorf("list sbom attestation attachments: %w", err)
	}
	missing, err := s.sbomAttestationAttachmentMissingEvidence(ctx, filter)
	if err != nil {
		return SBOMAttestationAttachmentPage{}, err
	}
	return SBOMAttestationAttachmentPage{
		Attachments:     out,
		MissingEvidence: missing,
	}, nil
}

const listSBOMAttestationAttachmentsQuery = `
SELECT fact.fact_id, fact.source_confidence, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ($2 = '' OR fact.payload->>'subject_digest' = $2)
  AND ($3 = '' OR fact.payload->>'document_id' = $3)
  AND ($4 = '' OR fact.payload->>'document_digest' = $4)
  AND ($5 = '' OR fact.payload->>'attachment_status' = $5)
  AND ($6 = '' OR fact.payload->>'artifact_kind' = $6)
  AND ($7 = '' OR fact.payload->'repository_ids' ? $7)
  AND ($8 = '' OR fact.payload->'workload_ids' ? $8)
  AND ($9 = '' OR fact.payload->'service_ids' ? $9)
  AND ($10 = '' OR fact.fact_id > $10)
  AND (
        COALESCE(cardinality($12::text[]), 0) = 0
        OR fact.payload->'repository_ids' ?| $12::text[]
      )
ORDER BY fact.fact_id ASC
LIMIT $11
`

const sbomAttestationAttachmentMissingEvidenceQuery = `
WITH active_images AS (
    SELECT DISTINCT support.digest
    FROM container_image_identity_current_supports AS support
    WHERE support.canonical_writes > 0
      AND support.outcome IN ('exact_digest', 'tag_resolved')
      AND ($1 = '' OR support.digest = $1)
      AND ($2 = '' OR $2 = ANY(support.source_repository_ids))
      AND ($3 = '' OR $3 = ANY(support.workload_ids))
      AND ($4 = '' OR $4 = ANY(support.service_ids))
      AND (
            COALESCE(cardinality($5::text[]), 0) = 0
            OR support.source_repository_ids && $5::text[]
          )
),
active_attachments AS (
    SELECT DISTINCT fact.payload->>'subject_digest' AS digest
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_sbom_attestation_attachment'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'subject_digest' <> ''
      AND ($1 = '' OR fact.payload->>'subject_digest' = $1)
      AND (
            COALESCE(cardinality($5::text[]), 0) = 0
            OR fact.payload->'repository_ids' ?| $5::text[]
          )
)
SELECT
    NOT EXISTS (SELECT 1 FROM active_images) AS missing_image,
    EXISTS (SELECT 1 FROM active_images)
      AND NOT EXISTS (
          SELECT 1
          FROM active_images AS image
          JOIN active_attachments AS attachment
            ON attachment.digest = image.digest
      ) AS missing_attachment
`

func (s PostgresSBOMAttestationAttachmentStore) sbomAttestationAttachmentMissingEvidence(
	ctx context.Context,
	filter SBOMAttestationAttachmentFilter,
) ([]string, error) {
	if filter.RepositoryID == "" && filter.WorkloadID == "" && filter.ServiceID == "" {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(
		ctx,
		sbomAttestationAttachmentMissingEvidenceQuery,
		filter.SubjectDigest,
		filter.RepositoryID,
		filter.WorkloadID,
		filter.ServiceID,
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("load sbom attestation attachment missing evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var missingImage bool
	var missingAttachment bool
	if err := rows.Scan(&missingImage, &missingAttachment); err != nil {
		return nil, fmt.Errorf("load sbom attestation attachment missing evidence: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load sbom attestation attachment missing evidence: %w", err)
	}
	var missing []string
	if missingImage {
		if filter.ServiceID != "" {
			missing = append(missing, "service_to_image_evidence_missing")
		}
		if filter.WorkloadID != "" {
			missing = append(missing, "workload_to_image_evidence_missing")
		}
		if filter.RepositoryID != "" || len(missing) == 0 {
			missing = append(missing, "repository_to_image_evidence_missing")
		}
	}
	if missingAttachment {
		missing = append(missing, "image_to_sbom_evidence_missing")
	}
	return uniqueSortedNonEmpty(missing), nil
}

func decodeSBOMAttestationAttachmentRow(
	factID string,
	sourceConfidence string,
	payloadBytes []byte,
) (SBOMAttestationAttachmentRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return SBOMAttestationAttachmentRow{}, fmt.Errorf("decode sbom attestation attachment: %w", err)
	}
	warnings, warningCount, warningsTruncated := boundedSBOMWarningSummariesFromValue(payload["warning_summaries"])
	if persistedCount := IntVal(payload, "warning_summary_count"); persistedCount > warningCount {
		warningCount = persistedCount
		warningsTruncated = true
	}
	dependencyRelationships := dependencyRelationshipRowsFromPayload(payload["dependency_relationship_evidence"])
	dependencyRelationshipCount := IntVal(payload, "dependency_relationship_count")
	externalReferences := externalReferenceRowsFromPayload(payload["external_reference_evidence"])
	externalReferenceCount := IntVal(payload, "external_reference_count")
	slsaMaterials := slsaMaterialRowsFromPayload(payload["slsa_provenance_materials"])
	slsaMaterialCount := IntVal(payload, "slsa_provenance_material_count")
	componentEvidence, componentCount, componentEvidenceTruncated := boundedComponentEvidenceRows(
		payload["component_evidence"],
		IntVal(payload, "component_count"),
	)
	return SBOMAttestationAttachmentRow{
		AttachmentID:                         factID,
		SubjectDigest:                        StringVal(payload, "subject_digest"),
		DocumentID:                           StringVal(payload, "document_id"),
		DocumentDigest:                       StringVal(payload, "document_digest"),
		AttachmentStatus:                     StringVal(payload, "attachment_status"),
		ParseStatus:                          StringVal(payload, "parse_status"),
		VerificationStatus:                   StringVal(payload, "verification_status"),
		VerificationPolicy:                   StringVal(payload, "verification_policy"),
		ArtifactKind:                         StringVal(payload, "artifact_kind"),
		Format:                               StringVal(payload, "format"),
		SpecVersion:                          StringVal(payload, "spec_version"),
		Reason:                               StringVal(payload, "reason"),
		AttachmentScope:                      StringVal(payload, "attachment_scope"),
		CanonicalWrites:                      IntVal(payload, "canonical_writes"),
		ComponentCount:                       componentCount,
		ComponentEvidence:                    componentEvidence,
		ComponentEvidenceTruncated:           componentEvidenceTruncated,
		DependencyRelationships:              dependencyRelationships,
		DependencyRelationshipCount:          dependencyRelationshipCount,
		DependencyRelationshipsTruncated:     dependencyRelationshipCount > len(dependencyRelationships),
		ExternalReferences:                   externalReferences,
		ExternalReferenceCount:               externalReferenceCount,
		ExternalReferencesTruncated:          externalReferenceCount > len(externalReferences),
		SLSAProvenancePredicateType:          StringVal(payload, "slsa_provenance_predicate_type"),
		SLSAProvenanceBuilderID:              StringVal(payload, "slsa_provenance_builder_id"),
		SLSAProvenanceMaterials:              slsaMaterials,
		SLSAProvenanceMaterialCount:          slsaMaterialCount,
		SLSAProvenanceMaterialsTruncated:     slsaMaterialCount > len(slsaMaterials),
		SLSAProvenanceConfigSourceURI:        StringVal(payload, "slsa_provenance_config_source_uri"),
		SLSAProvenanceConfigSourceEntryPoint: StringVal(payload, "slsa_provenance_config_source_entry_point"),
		SLSAProvenanceConfigSourceDigest:     stringMapVal(payload, "slsa_provenance_config_source_digest"),
		RepositoryIDs:                        StringSliceVal(payload, "repository_ids"),
		WorkloadIDs:                          StringSliceVal(payload, "workload_ids"),
		ServiceIDs:                           StringSliceVal(payload, "service_ids"),
		WarningSummaries:                     warnings,
		WarningSummaryCount:                  warningCount,
		WarningSummariesTruncated:            warningsTruncated,
		EvidenceFactIDs:                      StringSliceVal(payload, "evidence_fact_ids"),
		MissingEvidence:                      StringSliceVal(payload, "missing_evidence"),
		SourceFreshness:                      "active",
		SourceConfidence:                     sourceConfidence,
	}, nil
}
