package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SBOMAttestationAttachmentAggregateStore reads cheap-summary aggregates over
// reducer-owned SBOM and attestation attachments. It replaces the
// page-and-iterate caller workflow for ecosystem-level questions like "how
// many attestations are verified vs unverified?" or "which subjects have
// ambiguous SBOM attachment?" exposed by list_sbom_attestation_attachments.
type SBOMAttestationAttachmentAggregateStore interface {
	CountSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentAggregateFilter) (SBOMAttestationAttachmentAggregateCount, error)
	SBOMAttestationAttachmentInventory(
		context.Context,
		SBOMAttestationAttachmentAggregateFilter,
		SBOMAttestationAttachmentInventoryDimension,
		int,
		int,
	) ([]SBOMAttestationAttachmentInventoryRow, error)
}

// SBOMAttestationAttachmentInventoryDimension names the grouping dimension
// for the inventory aggregate. Each enum value names a payload field that
// has supporting partial indexes in
// `go/internal/storage/postgres/schema_fact_records.go`
// (subject_digest+attachment_status, attachment_status+artifact_kind,
// document_id, document_digest).
type SBOMAttestationAttachmentInventoryDimension string

const (
	// SBOMAttestationAttachmentInventoryByAttachmentStatus groups by reducer
	// attachment_status (attached_verified, attached_unverified,
	// attached_parse_only, subject_mismatch, ambiguous_subject,
	// unknown_subject, unparseable).
	SBOMAttestationAttachmentInventoryByAttachmentStatus SBOMAttestationAttachmentInventoryDimension = "attachment_status"
	// SBOMAttestationAttachmentInventoryByArtifactKind groups by artifact
	// kind (sbom / attestation).
	SBOMAttestationAttachmentInventoryByArtifactKind SBOMAttestationAttachmentInventoryDimension = "artifact_kind"
	// SBOMAttestationAttachmentInventoryBySubjectDigest groups by subject
	// digest. High cardinality but useful for "which subjects have the most
	// attachments?" prompts.
	SBOMAttestationAttachmentInventoryBySubjectDigest SBOMAttestationAttachmentInventoryDimension = "subject_digest"
)

// SBOMAttestationAttachmentAggregateMaxLimit caps inventory result pages.
const SBOMAttestationAttachmentAggregateMaxLimit = 500

// SBOMAttestationAttachmentAggregateFilter narrows aggregate reads. An
// aggregate without a scope is allowed because the dataset is already
// bounded to `fact_kind = 'reducer_sbom_attestation_attachment'` and the
// active-generation predicate at index lookup time.
type SBOMAttestationAttachmentAggregateFilter struct {
	SubjectDigest    string
	DocumentID       string
	DocumentDigest   string
	AttachmentStatus string
	ArtifactKind     string
}

// SBOMAttestationAttachmentAggregateCount is the cheap-summary totals envelope
// used by the count handler. ByAttachmentStatus and ByArtifactKind are
// pre-aggregated rollups so callers can answer "attachments per status" and
// "attachments per artifact kind" without a second round trip.
type SBOMAttestationAttachmentAggregateCount struct {
	TotalAttachments   int
	ByAttachmentStatus map[string]int
	ByArtifactKind     map[string]int
}

// SBOMAttestationAttachmentInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type SBOMAttestationAttachmentInventoryRow struct {
	Dimension SBOMAttestationAttachmentInventoryDimension `json:"dimension"`
	Value     string                                      `json:"value"`
	Count     int                                         `json:"count"`
}

// PostgresSBOMAttestationAttachmentAggregateStore reads aggregate counts
// directly from reducer-owned SBOM/attestation attachment facts.
type PostgresSBOMAttestationAttachmentAggregateStore struct {
	DB sbomAttestationAttachmentAggregateQueryer
}

type sbomAttestationAttachmentAggregateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// NewPostgresSBOMAttestationAttachmentAggregateStore creates the
// Postgres-backed aggregate store.
func NewPostgresSBOMAttestationAttachmentAggregateStore(
	db sbomAttestationAttachmentAggregateQueryer,
) PostgresSBOMAttestationAttachmentAggregateStore {
	return PostgresSBOMAttestationAttachmentAggregateStore{DB: db}
}

const sbomAttestationAttachmentAggregateTotalQuery = `
SELECT COUNT(*) AS total
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
  AND ($1 = '' OR fact.payload->>'subject_digest' = $1)
  AND ($2 = '' OR fact.payload->>'document_id' = $2)
  AND ($3 = '' OR fact.payload->>'document_digest' = $3)
  AND ($4 = '' OR fact.payload->>'attachment_status' = $4)
  AND ($5 = '' OR fact.payload->>'artifact_kind' = $5);
`

const sbomAttestationAttachmentAggregateGroupQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
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
  AND ($1 = '' OR fact.payload->>'subject_digest' = $1)
  AND ($2 = '' OR fact.payload->>'document_id' = $2)
  AND ($3 = '' OR fact.payload->>'document_digest' = $3)
  AND ($4 = '' OR fact.payload->>'attachment_status' = $4)
  AND ($5 = '' OR fact.payload->>'artifact_kind' = $5)
GROUP BY bucket;
`

const sbomAttestationAttachmentInventoryQueryTemplate = `
SELECT %s AS bucket, COUNT(*) AS bucket_count
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
  AND ($1 = '' OR fact.payload->>'subject_digest' = $1)
  AND ($2 = '' OR fact.payload->>'document_id' = $2)
  AND ($3 = '' OR fact.payload->>'document_digest' = $3)
  AND ($4 = '' OR fact.payload->>'attachment_status' = $4)
  AND ($5 = '' OR fact.payload->>'artifact_kind' = $5)
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $6 OFFSET $7;
`

// CountSBOMAttestationAttachments returns the cheap-summary totals envelope
// for the scoped attachment slice.
func (s PostgresSBOMAttestationAttachmentAggregateStore) CountSBOMAttestationAttachments(
	ctx context.Context,
	filter SBOMAttestationAttachmentAggregateFilter,
) (SBOMAttestationAttachmentAggregateCount, error) {
	if s.DB == nil {
		return SBOMAttestationAttachmentAggregateCount{}, fmt.Errorf("sbom attestation attachment aggregate database is required")
	}

	args := []any{
		filter.SubjectDigest,
		filter.DocumentID,
		filter.DocumentDigest,
		filter.AttachmentStatus,
		filter.ArtifactKind,
	}

	row := s.DB.QueryRowContext(ctx, sbomAttestationAttachmentAggregateTotalQuery, args...)
	var total sql.NullInt64
	if err := row.Scan(&total); err != nil {
		return SBOMAttestationAttachmentAggregateCount{}, fmt.Errorf("count sbom attestation attachments: %w", err)
	}

	out := SBOMAttestationAttachmentAggregateCount{
		TotalAttachments:   int(total.Int64),
		ByAttachmentStatus: map[string]int{},
		ByArtifactKind:     map[string]int{},
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'attachment_status', ''), 'unknown')", out.ByAttachmentStatus); err != nil {
		return SBOMAttestationAttachmentAggregateCount{}, err
	}
	if err := s.fillBuckets(ctx, args, "COALESCE(NULLIF(fact.payload->>'artifact_kind', ''), 'unknown')", out.ByArtifactKind); err != nil {
		return SBOMAttestationAttachmentAggregateCount{}, err
	}
	return out, nil
}

func (s PostgresSBOMAttestationAttachmentAggregateStore) fillBuckets(
	ctx context.Context,
	args []any,
	groupExpr string,
	dst map[string]int,
) error {
	q := fmt.Sprintf(sbomAttestationAttachmentAggregateGroupQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("group sbom attestation attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return fmt.Errorf("scan sbom attestation attachment bucket: %w", err)
		}
		dst[bucket] = int(bucketCount)
	}
	return rows.Err()
}

// SBOMAttestationAttachmentInventory returns a paginated grouped count along
// the requested dimension. Limit and offset must already be normalized by
// the caller.
func (s PostgresSBOMAttestationAttachmentAggregateStore) SBOMAttestationAttachmentInventory(
	ctx context.Context,
	filter SBOMAttestationAttachmentAggregateFilter,
	dimension SBOMAttestationAttachmentInventoryDimension,
	limit int,
	offset int,
) ([]SBOMAttestationAttachmentInventoryRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("sbom attestation attachment aggregate database is required")
	}
	groupExpr, err := sbomAttestationAttachmentInventoryGroupExpression(dimension)
	if err != nil {
		return nil, err
	}
	// The handler asks for one extra row to detect truncation, so the store
	// accepts up to MaxLimit+1 for that internal pagination probe.
	if limit <= 0 || limit > SBOMAttestationAttachmentAggregateMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", SBOMAttestationAttachmentAggregateMaxLimit+1)
	}
	if offset < 0 {
		offset = 0
	}
	q := fmt.Sprintf(sbomAttestationAttachmentInventoryQueryTemplate, groupExpr)
	rows, err := s.DB.QueryContext(
		ctx,
		q,
		filter.SubjectDigest,
		filter.DocumentID,
		filter.DocumentDigest,
		filter.AttachmentStatus,
		filter.ArtifactKind,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory sbom attestation attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]SBOMAttestationAttachmentInventoryRow, 0, limit)
	for rows.Next() {
		var bucket string
		var bucketCount int64
		if err := rows.Scan(&bucket, &bucketCount); err != nil {
			return nil, fmt.Errorf("scan sbom attestation attachment inventory row: %w", err)
		}
		out = append(out, SBOMAttestationAttachmentInventoryRow{
			Dimension: dimension,
			Value:     strings.TrimSpace(bucket),
			Count:     int(bucketCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sbom attestation attachment inventory rows: %w", err)
	}
	return out, nil
}

// sbomAttestationAttachmentInventoryGroupExpression maps the dimension enum
// to the safe SQL expression substituted into the inventory query template.
// Only known enum values are accepted, so the substitution stays
// parameter-safe; filter values flow through bound parameters only.
func sbomAttestationAttachmentInventoryGroupExpression(
	dimension SBOMAttestationAttachmentInventoryDimension,
) (string, error) {
	switch dimension {
	case SBOMAttestationAttachmentInventoryByAttachmentStatus:
		return "COALESCE(NULLIF(fact.payload->>'attachment_status', ''), 'unknown')", nil
	case SBOMAttestationAttachmentInventoryByArtifactKind:
		return "COALESCE(NULLIF(fact.payload->>'artifact_kind', ''), 'unknown')", nil
	case SBOMAttestationAttachmentInventoryBySubjectDigest:
		return "COALESCE(NULLIF(fact.payload->>'subject_digest', ''), 'unknown')", nil
	default:
		return "", fmt.Errorf("unsupported sbom attestation attachment inventory dimension: %q", dimension)
	}
}

// isSupportedSBOMAttachmentStatus rejects unknown attachment_status filters
// using the same closed enum the list endpoint advertises.
func isSupportedSBOMAttachmentStatus(status string) bool {
	switch status {
	case "attached_verified",
		"attached_unverified",
		"attached_parse_only",
		"subject_mismatch",
		"ambiguous_subject",
		"unknown_subject",
		"unparseable":
		return true
	default:
		return false
	}
}

// isSupportedSBOMArtifactKind rejects unknown artifact_kind filters using the
// same closed enum the list endpoint advertises.
func isSupportedSBOMArtifactKind(kind string) bool {
	switch kind {
	case "sbom", "attestation":
		return true
	default:
		return false
	}
}
