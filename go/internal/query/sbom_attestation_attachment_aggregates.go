// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
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
// active-generation predicate at index lookup time. Source anchors narrow the
// read to reducer-owned repository, workload, or service evidence without
// inventing image attachment truth.
type SBOMAttestationAttachmentAggregateFilter struct {
	SubjectDigest    string
	DocumentID       string
	DocumentDigest   string
	RepositoryID     string
	WorkloadID       string
	ServiceID        string
	AttachmentStatus string
	ArtifactKind     string
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (union of
	// granted repository and ingestion-scope ids). When populated, aggregate
	// counts, inventory buckets, and the missing-evidence probe cover only
	// attachments whose repository_ids overlap the grant set.
	AllowedSourceRepositoryIDs []string
}

// SBOMAttestationAttachmentAggregateCount is the cheap-summary totals envelope
// used by the count handler. ByAttachmentStatus and ByArtifactKind are
// pre-aggregated rollups so callers can answer "attachments per status" and
// "attachments per artifact kind" without a second round trip. MissingEvidence
// carries source-scope gap classes for zero or incomplete target readbacks.
type SBOMAttestationAttachmentAggregateCount struct {
	TotalAttachments   int
	ByAttachmentStatus map[string]int
	ByArtifactKind     map[string]int
	MissingEvidence    []string
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

// sbomAttestationAttachmentAggregateRollupQuery computes the count handler's
// total, per-attachment_status, and per-artifact_kind rollups in one scan via
// GROUP BY GROUPING SETS. #3389: the previous count handler ran three separate
// queries (one COUNT(*) plus two GROUP BY) that each scanned the active
// attachment facts, so the count endpoint paid three full scans and three round
// trips. GROUPING SETS folds all three rollups into one pass over the same
// #3402-indexed active tuples; the GROUPING() flags tag which set each row
// belongs to (status bucket, kind bucket, or grand total) so the Go side can
// partition without a second query. The single-fact_kind + is_tombstone +
// active-generation anchor is unchanged, so the #3402 active_scan partial index
// stays eligible and results are identical to the three-query shape.
const sbomAttestationAttachmentAggregateRollupQuery = `
SELECT
    GROUPING(COALESCE(NULLIF(fact.payload->>'attachment_status', ''), 'unknown')) AS grouping_status,
    GROUPING(COALESCE(NULLIF(fact.payload->>'artifact_kind', ''), 'unknown')) AS grouping_kind,
    COALESCE(NULLIF(fact.payload->>'attachment_status', ''), 'unknown') AS attachment_status,
    COALESCE(NULLIF(fact.payload->>'artifact_kind', ''), 'unknown') AS artifact_kind,
    COUNT(*) AS bucket_count
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
  AND ($6 = '' OR fact.payload->'repository_ids' ? $6)
  AND ($7 = '' OR fact.payload->'workload_ids' ? $7)
  AND ($8 = '' OR fact.payload->'service_ids' ? $8)
  AND (
        COALESCE(cardinality($9::text[]), 0) = 0
        OR fact.payload->'repository_ids' ?| $9::text[]
      )
GROUP BY GROUPING SETS (
    (COALESCE(NULLIF(fact.payload->>'attachment_status', ''), 'unknown')),
    (COALESCE(NULLIF(fact.payload->>'artifact_kind', ''), 'unknown')),
    ()
);
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
  AND ($6 = '' OR fact.payload->'repository_ids' ? $6)
  AND ($7 = '' OR fact.payload->'workload_ids' ? $7)
  AND ($8 = '' OR fact.payload->'service_ids' ? $8)
  AND (
        COALESCE(cardinality($9::text[]), 0) = 0
        OR fact.payload->'repository_ids' ?| $9::text[]
      )
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $10 OFFSET $11;
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
		filter.RepositoryID,
		filter.WorkloadID,
		filter.ServiceID,
		pq.Array(filter.AllowedSourceRepositoryIDs),
	}

	rows, err := s.DB.QueryContext(ctx, sbomAttestationAttachmentAggregateRollupQuery, args...)
	if err != nil {
		return SBOMAttestationAttachmentAggregateCount{}, fmt.Errorf("count sbom attestation attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	rollup := make([]sbomAttestationAttachmentRollupRow, 0, 16)
	for rows.Next() {
		var r sbomAttestationAttachmentRollupRow
		if err := rows.Scan(&r.groupingStatus, &r.groupingKind, &r.attachmentStatus, &r.artifactKind, &r.count); err != nil {
			return SBOMAttestationAttachmentAggregateCount{}, fmt.Errorf("scan sbom attestation attachment rollup: %w", err)
		}
		rollup = append(rollup, r)
	}
	if err := rows.Err(); err != nil {
		return SBOMAttestationAttachmentAggregateCount{}, fmt.Errorf("count sbom attestation attachments: %w", err)
	}

	out := buildSBOMAttestationAttachmentAggregateCount(rollup)
	missing, err := s.sbomAttestationAttachmentAggregateMissingEvidence(ctx, filter)
	if err != nil {
		return SBOMAttestationAttachmentAggregateCount{}, err
	}
	out.MissingEvidence = missing
	return out, nil
}

// sbomAttestationAttachmentRollupRow is one GROUPING SETS row from
// sbomAttestationAttachmentAggregateRollupQuery. groupingStatus/groupingKind are
// the GROUPING() flags: 0 means the column is part of this row's grouping set, 1
// means it is rolled up (NULL) for this set. attachmentStatus and artifactKind
// are sql.NullString because GROUPING SETS sets the unselected grouping column to
// NULL on rolled-up rows (the grand-total row has both NULL); scanning NULL into a
// plain string causes "converting NULL to string is unsupported" (#3547).
type sbomAttestationAttachmentRollupRow struct {
	groupingStatus   int
	groupingKind     int
	attachmentStatus sql.NullString
	artifactKind     sql.NullString
	count            int64
}

// buildSBOMAttestationAttachmentAggregateCount partitions the GROUPING SETS rows
// into the count envelope. A row with groupingStatus=1 and groupingKind=1 is the
// grand total; groupingStatus=0 rows are attachment_status buckets;
// groupingKind=0 rows are artifact_kind buckets. The result is identical to the
// previous COUNT(*) + two GROUP BY query trio.
func buildSBOMAttestationAttachmentAggregateCount(
	rows []sbomAttestationAttachmentRollupRow,
) SBOMAttestationAttachmentAggregateCount {
	out := SBOMAttestationAttachmentAggregateCount{
		ByAttachmentStatus: map[string]int{},
		ByArtifactKind:     map[string]int{},
	}
	for _, r := range rows {
		switch {
		case r.groupingStatus == 1 && r.groupingKind == 1:
			out.TotalAttachments = int(r.count)
		case r.groupingStatus == 0:
			out.ByAttachmentStatus[r.attachmentStatus.String] = int(r.count)
		case r.groupingKind == 0:
			out.ByArtifactKind[r.artifactKind.String] = int(r.count)
		}
	}
	return out
}

func (s PostgresSBOMAttestationAttachmentAggregateStore) sbomAttestationAttachmentAggregateMissingEvidence(
	ctx context.Context,
	filter SBOMAttestationAttachmentAggregateFilter,
) ([]string, error) {
	store := PostgresSBOMAttestationAttachmentStore{DB: s.DB}
	return store.sbomAttestationAttachmentMissingEvidence(ctx, SBOMAttestationAttachmentFilter{
		SubjectDigest:              filter.SubjectDigest,
		RepositoryID:               filter.RepositoryID,
		WorkloadID:                 filter.WorkloadID,
		ServiceID:                  filter.ServiceID,
		AllowedSourceRepositoryIDs: filter.AllowedSourceRepositoryIDs,
	})
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
		filter.RepositoryID,
		filter.WorkloadID,
		filter.ServiceID,
		pq.Array(filter.AllowedSourceRepositoryIDs),
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
