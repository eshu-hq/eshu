// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
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

func SupplyChainImpactExplanationAmbiguousCandidateCount(err error) int {
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
	filter = TrimSupplyChainImpactExplanationFilter(filter)
	if !filter.HasBoundedScope() {
		return SupplyChainImpactExplanationRow{}, fmt.Errorf("finding_id or advisory/cve plus package, repository, or subject digest is required")
	}
	args := supplyChainImpactExplanationQueryArgs(filter, s.Now)
	query := ExplainSupplyChainImpactFindingQuery
	if filter.FindingID != "" {
		query = ExplainSupplyChainImpactFindingByPublicIDQuery
	}
	findings, err := s.loadSupplyChainImpactExplanationFindings(ctx, query, args)
	if err != nil {
		return SupplyChainImpactExplanationRow{}, err
	}
	if filter.FindingID != "" && len(findings) == 0 {
		findings, err = s.loadSupplyChainImpactExplanationFindings(
			ctx,
			ExplainSupplyChainImpactFindingQuery,
			args,
		)
		if err != nil {
			return SupplyChainImpactExplanationRow{}, err
		}
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

func (s PostgresSupplyChainImpactFindingStore) loadSupplyChainImpactExplanationFindings(
	ctx context.Context,
	query string,
	args []any,
) ([]SupplyChainImpactFindingRow, error) {
	rows, err := s.DB.QueryContext(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("explain supply chain impact finding: %w", err)
	}
	defer func() { _ = rows.Close() }()

	findings := make([]SupplyChainImpactFindingRow, 0, 2)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("explain supply chain impact finding: %w", err)
		}
		finding, err := DecodeSupplyChainImpactFindingRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("explain supply chain impact finding: %w", err)
	}
	return findings, nil
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
		pgarray.Array(factIDs),
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
		var schemaVersion sql.NullString
		var payloadBytes []byte
		if err := rows.Scan(
			&fact.FactID,
			&fact.FactKind,
			&sourceSystem,
			&sourceConfidence,
			&observedAt,
			&schemaVersion,
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
		if schemaVersion.Valid {
			fact.SchemaVersion = schemaVersion.String
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

var ExplainSupplyChainImpactFindingByPublicIDQuery = buildExplainSupplyChainImpactFindingQuery(
	`
    AND fact.payload->>'finding_id' = $2`,
	"",
)

var ExplainSupplyChainImpactFindingQuery = buildExplainSupplyChainImpactFindingQuery(
	"",
	`
  WHERE $2 = ''
     OR fact_id = $2
     OR finding_id = $2
     OR canonical_key = $2
     OR canonical_key IN (
          SELECT `+SupplyChainImpactCanonicalFindingKeySQL+`
          FROM fact_records AS fact
          JOIN ingestion_scopes AS identity_scope
            ON identity_scope.scope_id = fact.scope_id
           AND identity_scope.active_generation_id = fact.generation_id
          JOIN scope_generations AS identity_generation
            ON identity_generation.scope_id = fact.scope_id
           AND identity_generation.generation_id = fact.generation_id
          WHERE fact.fact_kind = $1
            AND fact.is_tombstone = FALSE
            AND identity_generation.status = 'active'
            AND fact.fact_id = $2
        )`,
)

func buildExplainSupplyChainImpactFindingQuery(
	authorizedSourcePredicate string,
	sourceCandidatePredicate string,
) string {
	return `
WITH ` + supplyChainImpactRuntimeFilterCTE("$9", "$8", "''", "$11", "$12") + `,
authorized_source_candidates AS NOT MATERIALIZED (
  SELECT fact.fact_id,
         fact.scope_id,
         ` + supplyChainImpactPublicFindingIDSQL + ` AS finding_id,
         fact.source_confidence,
         fact.payload,
         COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state,
         COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score,
         ` + supplyChainImpactPayloadFindingIDPresentSQL + ` AS has_payload_finding_id,
         ` + SupplyChainImpactCanonicalFindingKeySQL + ` AS canonical_key
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
    AND fact.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
    AND ($3 = '' OR fact.payload->>'advisory_id' = $3 OR fact.payload->>'cve_id' = $3)
    AND ($4 = '' OR fact.payload->>'cve_id' = $4)
    AND ($5 = '' OR fact.payload->>'package_id' = $5)
    AND ($6 = '' OR fact.payload->>'repository_id' = $6)
    AND ($7 = '' OR fact.payload->>'subject_digest' = $7)
` + supplyChainImpactRuntimeFilterPredicate(
		"fact.payload->>'repository_id'",
		"$9",
		"$8",
		"''",
	) + `
    AND ($10 = '' OR fact.payload->>'image_ref' = $10)
    AND (
      (COALESCE(cardinality($11::text[]), 0) = 0 AND COALESCE(cardinality($12::text[]), 0) = 0)
      OR fact.payload->>'repository_id' = ANY($11::text[])
      OR fact.scope_id = ANY($12::text[])
    )
` + authorizedSourcePredicate + `
),
source_candidates AS MATERIALIZED (
  SELECT *
  FROM authorized_source_candidates
` + sourceCandidatePredicate + `
),
` + supplyChainImpactBoundedOperatorCandidatesCTE("$1") + `,
` + supplyChainImpactSplitCanonicalWinnerCTEs("$13::timestamptz") + `,
canonical_facts AS (
  SELECT finding_id, source_confidence, payload
  FROM canonical_winners
)
SELECT finding_id, source_confidence, payload
FROM canonical_facts
ORDER BY finding_id ASC
LIMIT 2
`
}

func supplyChainImpactExplanationQueryArgs(
	filter SupplyChainImpactExplanationFilter,
	now func() time.Time,
) []any {
	return []any{
		SupplyChainImpactFindingFactKind,
		filter.FindingID,
		filter.AdvisoryID,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.WorkloadID,
		filter.ServiceID,
		filter.ImageRef,
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
		SupplyChainImpactSuppressionReadAt(now),
	}
}

const explainSupplyChainImpactEvidenceFactsQuery = `
SELECT fact.fact_id, fact.fact_kind, fact.source_system, fact.source_confidence, fact.observed_at, fact.schema_version, fact.payload
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

func TrimSupplyChainImpactExplanationFilter(
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
