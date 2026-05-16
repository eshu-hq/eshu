package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const sbomAttestationAttachmentFactKind = "reducer_sbom_attestation_attachment"

// SBOMAttestationAttachmentStore reads reducer-owned SBOM and attestation
// attachment facts.
type SBOMAttestationAttachmentStore interface {
	ListSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentFilter) ([]SBOMAttestationAttachmentRow, error)
}

// SBOMAttestationAttachmentFilter bounds attachment reads to a concrete image
// digest or document identity.
type SBOMAttestationAttachmentFilter struct {
	SubjectDigest     string
	DocumentID        string
	DocumentDigest    string
	AttachmentStatus  string
	ArtifactKind      string
	AfterAttachmentID string
	Limit             int
}

// ComponentEvidenceRow exposes bounded SBOM component evidence attached to a
// document without implying vulnerability impact.
type ComponentEvidenceRow struct {
	ComponentID string `json:"component_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	PURL        string `json:"purl,omitempty"`
	CPE         string `json:"cpe,omitempty"`
	FactID      string `json:"fact_id,omitempty"`
}

// SBOMAttestationAttachmentRow is one durable SBOM attachment fact decoded from
// the reducer-owned read model.
type SBOMAttestationAttachmentRow struct {
	AttachmentID       string
	SubjectDigest      string
	DocumentID         string
	DocumentDigest     string
	AttachmentStatus   string
	ParseStatus        string
	VerificationStatus string
	VerificationPolicy string
	ArtifactKind       string
	Format             string
	SpecVersion        string
	Reason             string
	CanonicalWrites    int
	ComponentCount     int
	ComponentEvidence  []ComponentEvidenceRow
	WarningSummaries   []string
	EvidenceFactIDs    []string
	SourceFreshness    string
	SourceConfidence   string
}

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
) ([]SBOMAttestationAttachmentRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("sbom attestation attachment database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("subject_digest, document_id, or document_digest is required")
	}
	if filter.Limit <= 0 || filter.Limit > sbomAttestationAttachmentMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", sbomAttestationAttachmentMaxLimit)
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
		filter.AfterAttachmentID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sbom attestation attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SBOMAttestationAttachmentRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list sbom attestation attachments: %w", err)
		}
		row, err := decodeSBOMAttestationAttachmentRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sbom attestation attachments: %w", err)
	}
	return out, nil
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
  AND ($7 = '' OR fact.fact_id > $7)
ORDER BY fact.fact_id ASC
LIMIT $8
`

func (f SBOMAttestationAttachmentFilter) hasScope() bool {
	return f.SubjectDigest != "" || f.DocumentID != "" || f.DocumentDigest != ""
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
	return SBOMAttestationAttachmentRow{
		AttachmentID:       factID,
		SubjectDigest:      StringVal(payload, "subject_digest"),
		DocumentID:         StringVal(payload, "document_id"),
		DocumentDigest:     StringVal(payload, "document_digest"),
		AttachmentStatus:   StringVal(payload, "attachment_status"),
		ParseStatus:        StringVal(payload, "parse_status"),
		VerificationStatus: StringVal(payload, "verification_status"),
		VerificationPolicy: StringVal(payload, "verification_policy"),
		ArtifactKind:       StringVal(payload, "artifact_kind"),
		Format:             StringVal(payload, "format"),
		SpecVersion:        StringVal(payload, "spec_version"),
		Reason:             StringVal(payload, "reason"),
		CanonicalWrites:    IntVal(payload, "canonical_writes"),
		ComponentCount:     IntVal(payload, "component_count"),
		ComponentEvidence:  componentEvidenceRows(payload["component_evidence"]),
		WarningSummaries:   StringSliceVal(payload, "warning_summaries"),
		EvidenceFactIDs:    StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:    "active",
		SourceConfidence:   sourceConfidence,
	}, nil
}

func componentEvidenceRows(raw any) []ComponentEvidenceRow {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ComponentEvidenceRow, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, ComponentEvidenceRow{
			ComponentID: StringVal(row, "component_id"),
			Name:        StringVal(row, "name"),
			Version:     StringVal(row, "version"),
			PURL:        StringVal(row, "purl"),
			CPE:         StringVal(row, "cpe"),
			FactID:      StringVal(row, "fact_id"),
		})
	}
	return out
}
