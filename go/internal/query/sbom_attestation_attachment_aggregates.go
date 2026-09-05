// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

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
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
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
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
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
