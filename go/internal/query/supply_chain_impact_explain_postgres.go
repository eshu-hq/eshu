// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

var (
	// ErrSupplyChainImpactExplanationNotFound means the bounded explain scope
	// did not match an active reducer-owned impact finding.
	ErrSupplyChainImpactExplanationNotFound = errors.New("supply chain impact explanation not found")
	// ErrSupplyChainImpactExplanationAmbiguous means the bounded explain scope
	// matched more than one active finding and needs a narrower anchor.
	ErrSupplyChainImpactExplanationAmbiguous = errors.New("supply chain impact explanation scope is ambiguous")
)

type supplyChainImpactExplanationAmbiguousError struct {
	candidateCount int
}

func (e *supplyChainImpactExplanationAmbiguousError) Error() string {
	return ErrSupplyChainImpactExplanationAmbiguous.Error()
}

func (e *supplyChainImpactExplanationAmbiguousError) Is(target error) bool {
	return target == ErrSupplyChainImpactExplanationAmbiguous
}

func newSupplyChainImpactExplanationAmbiguousError(candidateCount int) error {
	if candidateCount < 2 {
		candidateCount = 2
	}
	return &supplyChainImpactExplanationAmbiguousError{candidateCount: candidateCount}
}

func supplyChainImpactExplanationAmbiguousCandidateCount(err error) int {
	var ambiguous *supplyChainImpactExplanationAmbiguousError
	if errors.As(err, &ambiguous) && ambiguous.candidateCount > 0 {
		return ambiguous.candidateCount
	}
	if errors.Is(err, ErrSupplyChainImpactExplanationAmbiguous) {
		return 2
	}
	return 0
}

// ExplainSupplyChainImpact returns exactly one active impact finding plus the
// evidence fact previews referenced by the finding.
func (s PostgresSupplyChainImpactFindingStore) ExplainSupplyChainImpact(
	ctx context.Context,
	filter SupplyChainImpactExplanationFilter,
) (SupplyChainImpactExplanationRow, error) {
	if s.DB == nil {
		return SupplyChainImpactExplanationRow{}, fmt.Errorf("supply chain impact finding database is required")
	}
	filter = trimSupplyChainImpactExplanationFilter(filter)
	if !filter.hasBoundedScope() {
		return SupplyChainImpactExplanationRow{}, fmt.Errorf("finding_id or advisory/cve plus package, repository, or subject digest is required")
	}
	rows, err := s.DB.QueryContext(
		ctx,
		explainSupplyChainImpactFindingQuery,
		supplyChainImpactFindingFactKind,
		filter.FindingID,
		filter.AdvisoryID,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.WorkloadID,
		filter.ServiceID,
		filter.ImageRef,
	)
	if err != nil {
		return SupplyChainImpactExplanationRow{}, fmt.Errorf("explain supply chain impact finding: %w", err)
	}
	defer func() { _ = rows.Close() }()

	findings := make([]SupplyChainImpactFindingRow, 0, 2)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return SupplyChainImpactExplanationRow{}, fmt.Errorf("explain supply chain impact finding: %w", err)
		}
		finding, err := decodeSupplyChainImpactFindingRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return SupplyChainImpactExplanationRow{}, err
		}
		findings = append(findings, finding)
	}
	if err := rows.Err(); err != nil {
		return SupplyChainImpactExplanationRow{}, fmt.Errorf("explain supply chain impact finding: %w", err)
	}
	switch len(findings) {
	case 0:
		return SupplyChainImpactExplanationRow{}, ErrSupplyChainImpactExplanationNotFound
	case 1:
	default:
		return SupplyChainImpactExplanationRow{}, newSupplyChainImpactExplanationAmbiguousError(len(findings))
	}
	evidence, err := s.loadSupplyChainImpactEvidenceFacts(ctx, findings[0].EvidenceFactIDs)
	if err != nil {
		return SupplyChainImpactExplanationRow{}, err
	}
	return SupplyChainImpactExplanationRow{
		Finding:       findings[0],
		EvidenceFacts: evidence,
	}, nil
}

func (s PostgresSupplyChainImpactFindingStore) loadSupplyChainImpactEvidenceFacts(
	ctx context.Context,
	factIDs []string,
) ([]SupplyChainImpactEvidenceFact, error) {
	factIDs = explanationUniqueStrings(factIDs)
	if len(factIDs) == 0 {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(
		ctx,
		explainSupplyChainImpactEvidenceFactsQuery,
		pq.Array(factIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("explain supply chain impact evidence facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SupplyChainImpactEvidenceFact, 0, len(factIDs))
	for rows.Next() {
		var fact SupplyChainImpactEvidenceFact
		var sourceSystem sql.NullString
		var sourceConfidence sql.NullString
		var observedAt sql.NullTime
		var payloadBytes []byte
		if err := rows.Scan(
			&fact.FactID,
			&fact.FactKind,
			&sourceSystem,
			&sourceConfidence,
			&observedAt,
			&payloadBytes,
		); err != nil {
			return nil, fmt.Errorf("explain supply chain impact evidence facts: %w", err)
		}
		if sourceSystem.Valid {
			fact.SourceSystem = sourceSystem.String
		}
		if sourceConfidence.Valid {
			fact.SourceConfidence = sourceConfidence.String
		}
		if observedAt.Valid {
			fact.ObservedAt = observedAt.Time.UTC()
		}
		if err := json.Unmarshal(payloadBytes, &fact.Payload); err != nil {
			return nil, fmt.Errorf("decode supply chain impact evidence fact %q: %w", fact.FactID, err)
		}
		out = append(out, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("explain supply chain impact evidence facts: %w", err)
	}
	return out, nil
}

const explainSupplyChainImpactFindingQuery = `
WITH raw_facts AS (
SELECT fact.fact_id,
       ` + supplyChainImpactPublicFindingIDSQL + ` AS finding_id,
       fact.source_confidence,
       fact.payload,
       COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
       ` + supplyChainImpactPayloadFindingIDPresentSQL + ` AS has_payload_finding_id,
       ` + supplyChainImpactCanonicalFindingKeySQL + ` AS canonical_key
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
  AND ($3 = '' OR fact.payload->>'advisory_id' = $3 OR fact.payload->>'cve_id' = $3)
  AND ($4 = '' OR fact.payload->>'cve_id' = $4)
  AND ($5 = '' OR fact.payload->>'package_id' = $5)
  AND ($6 = '' OR fact.payload->>'repository_id' = $6)
  AND ($7 = '' OR fact.payload->>'subject_digest' = $7)
  AND ($8 = '' OR fact.payload->'workload_ids' ? $8)
  AND ($9 = '' OR fact.payload->'service_ids' ? $9)
  AND ($10 = '' OR fact.payload->>'image_ref' = $10)
),
scoped_facts AS (
SELECT *
FROM raw_facts
WHERE $2 = ''
   OR fact_id = $2
   OR finding_id = $2
   OR canonical_key = $2
),
ranked_facts AS (
SELECT *,
       ROW_NUMBER() OVER (
         PARTITION BY canonical_key
         ORDER BY priority_score DESC, has_payload_finding_id DESC, fact_id ASC
       ) AS canonical_rank
FROM scoped_facts
),
canonical_facts AS (
SELECT finding_id, source_confidence, payload
FROM ranked_facts
WHERE canonical_rank = 1
)
SELECT finding_id, source_confidence, payload
FROM canonical_facts
ORDER BY finding_id ASC
LIMIT 2
`

const explainSupplyChainImpactEvidenceFactsQuery = `
SELECT fact.fact_id, fact.fact_kind, fact.source_system, fact.source_confidence, fact.observed_at, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_id = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
ORDER BY fact.fact_id ASC
`

func trimSupplyChainImpactExplanationFilter(
	filter SupplyChainImpactExplanationFilter,
) SupplyChainImpactExplanationFilter {
	filter.FindingID = strings.TrimSpace(filter.FindingID)
	filter.AdvisoryID = strings.TrimSpace(filter.AdvisoryID)
	filter.CVEID = strings.TrimSpace(filter.CVEID)
	filter.PackageID = strings.TrimSpace(filter.PackageID)
	filter.RepositoryID = strings.TrimSpace(filter.RepositoryID)
	filter.SubjectDigest = strings.TrimSpace(filter.SubjectDigest)
	filter.ImageRef = strings.TrimSpace(filter.ImageRef)
	filter.WorkloadID = strings.TrimSpace(filter.WorkloadID)
	filter.ServiceID = strings.TrimSpace(filter.ServiceID)
	return filter
}
