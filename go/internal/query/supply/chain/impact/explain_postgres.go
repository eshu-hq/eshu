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

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var (
	// ErrExplanationNotFound means the bounded explain scope
	// did not match an active reducer-owned impact finding.
	ErrExplanationNotFound = errors.New("supply chain impact explanation not found")
	// ErrExplanationAmbiguous means the bounded explain scope
	// matched more than one active finding and needs a narrower anchor.
	ErrExplanationAmbiguous = errors.New("supply chain impact explanation scope is ambiguous")
)

type supplyChainImpactExplanationAmbiguousError struct {
	candidateCount int
}

func (e *supplyChainImpactExplanationAmbiguousError) Error() string {
	return ErrExplanationAmbiguous.Error()
}

func (e *supplyChainImpactExplanationAmbiguousError) Is(target error) bool {
	return target == ErrExplanationAmbiguous
}

func newSupplyChainImpactExplanationAmbiguousError(candidateCount int) error {
	if candidateCount < 2 {
		candidateCount = 2
	}
	return &supplyChainImpactExplanationAmbiguousError{candidateCount: candidateCount}
}

// ExplanationAmbiguousCandidateCount reports how many candidates an
// ambiguous-scope explain error matched, or 0 when err is not that error.
func ExplanationAmbiguousCandidateCount(err error) int {
	var ambiguous *supplyChainImpactExplanationAmbiguousError
	if errors.As(err, &ambiguous) && ambiguous.candidateCount > 0 {
		return ambiguous.candidateCount
	}
	if errors.Is(err, ErrExplanationAmbiguous) {
		return 2
	}
	return 0
}

// ExplainSupplyChainImpact returns exactly one active impact finding plus the
// evidence fact previews referenced by the finding.
func (s PostgresFindingStore) ExplainSupplyChainImpact(
	ctx context.Context,
	filter ExplanationFilter,
) (ExplanationRow, error) {
	if s.DB == nil && s.reader == nil {
		return ExplanationRow{}, fmt.Errorf("supply chain impact finding database is required")
	}
	filter = TrimExplanationFilter(filter)
	if !filter.HasBoundedScope() {
		return ExplanationRow{}, fmt.Errorf("finding_id or advisory/cve plus package, repository, or subject digest is required")
	}
	readStore, commit, err := s.explanationSnapshot(ctx)
	if err != nil {
		return ExplanationRow{}, err
	}
	if commit != nil {
		defer func() { _ = commit(false) }()
	}
	args := supplyChainImpactExplanationQueryArgs(ctx, filter, s.Now)
	query := ExplainFindingQuery
	if filter.FindingID != "" {
		query = ExplainFindingByPublicIDQuery
	}
	findings, err := readStore.loadSupplyChainImpactExplanationFindings(ctx, query, args)
	if err != nil {
		return ExplanationRow{}, err
	}
	if filter.FindingID != "" && len(findings) == 0 {
		findings, err = readStore.loadSupplyChainImpactExplanationFindings(
			ctx,
			ExplainFindingQuery,
			args,
		)
		if err != nil {
			return ExplanationRow{}, err
		}
	}
	switch len(findings) {
	case 0:
		return ExplanationRow{}, ErrExplanationNotFound
	case 1:
	default:
		return ExplanationRow{}, newSupplyChainImpactExplanationAmbiguousError(len(findings))
	}
	evidence, err := readStore.loadSupplyChainImpactAuthorizedEvidenceFacts(ctx, findings[0].factID, findings[0].finding.EvidenceFactIDs)
	if err != nil {
		return ExplanationRow{}, err
	}
	if commit != nil {
		if err := commit(true); err != nil {
			return ExplanationRow{}, fmt.Errorf("commit supply chain impact explanation snapshot: %w", err)
		}
	}
	return ExplanationRow{
		Finding:       findings[0].finding,
		EvidenceFacts: evidence,
	}, nil
}

type supplyChainImpactExplanationCandidate struct {
	factID  string
	finding FindingRow
}

// explanationSnapshot holds winner selection and evidence hydration on one
// physical repeatable-read snapshot when the read port supports transactions.
func (s PostgresFindingStore) explanationSnapshot(
	ctx context.Context,
) (PostgresFindingStore, func(bool) error, error) {
	if beginner, ok := s.reader.(db.ReadSnapshotBeginner); ok {
		tx, err := beginner.BeginReadOnlySnapshot(ctx)
		if err != nil {
			return s, nil, fmt.Errorf("begin supply chain impact explanation snapshot: %w", err)
		}
		s.reader = tx
		return s, func(success bool) error {
			if success {
				return tx.Commit()
			}
			return tx.Rollback()
		}, nil
	}
	if beginner, ok := s.DB.(readinessTransactionStarter); ok {
		tx, err := beginner.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			return s, nil, fmt.Errorf("begin supply chain impact explanation snapshot: %w", err)
		}
		s.reader = legacyFindingQueryer{database: tx}
		return s, func(success bool) error {
			if success {
				return tx.Commit()
			}
			return tx.Rollback()
		}, nil
	}
	// Legacy test doubles expose only QueryContext; derived candidates remain
	// closed until the protected publication writer is available.
	return s, nil, nil
}

func (s PostgresFindingStore) loadSupplyChainImpactExplanationFindings(
	ctx context.Context,
	query string,
	args []any,
) ([]supplyChainImpactExplanationCandidate, error) {
	rows, err := s.queryer().QueryContext(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("explain supply chain impact finding: %w", err)
	}
	defer func() { _ = rows.Close() }()

	findings := make([]supplyChainImpactExplanationCandidate, 0, 2)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("explain supply chain impact finding: %w", err)
		}
		finding, err := DecodeFindingRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return nil, err
		}
		findings = append(findings, supplyChainImpactExplanationCandidate{factID: factID, finding: finding})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("explain supply chain impact finding: %w", err)
	}
	return findings, nil
}

func (s PostgresFindingStore) loadSupplyChainImpactAuthorizedEvidenceFacts(
	ctx context.Context, findingFactID string, factIDs []string,
) ([]EvidenceFact, error) {
	return s.loadSupplyChainImpactEvidenceFactsQuery(
		ctx, explainSupplyChainImpactAuthorizedEvidenceFactsQuery,
		[]any{findingFactID, array.Of(explanationUniqueStrings(factIDs))}, len(factIDs),
	)
}

func (s PostgresFindingStore) loadSupplyChainImpactEvidenceFacts(
	ctx context.Context,
	factIDs []string,
) ([]EvidenceFact, error) {
	factIDs = explanationUniqueStrings(factIDs)
	return s.loadSupplyChainImpactEvidenceFactsQuery(
		ctx, explainSupplyChainImpactEvidenceFactsQuery,
		[]any{array.Of(factIDs)}, len(factIDs),
	)
}

func (s PostgresFindingStore) loadSupplyChainImpactEvidenceFactsQuery(
	ctx context.Context, query string, args []any, capacity int,
) ([]EvidenceFact, error) {
	if capacity == 0 {
		return nil, nil
	}
	rows, err := s.queryer().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("explain supply chain impact evidence facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]EvidenceFact, 0, capacity)
	for rows.Next() {
		var fact EvidenceFact
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

// ExplainFindingByPublicIDQuery explains one finding anchored on its exact
// public finding_id, never falling back to a canonical-key candidate match.
var ExplainFindingByPublicIDQuery = supplyChainImpactReadModeSQL(buildExplainSupplyChainImpactFindingQuery(
	`
    AND fact.payload->>'finding_id' = $2`,
	"",
), false)

// ExplainFindingQuery explains a finding from a caller-supplied identifier
// that may be a fact id, finding id, or canonical key: an empty $2 applies
// no identifier filter (every finding in the bounded scope matches), and a
// non-empty $2 matches a direct fact id, finding id, or canonical key, OR'd
// together with a canonical-key candidate match rather than falling back to
// it. Without a finding_id the store runs this query directly; with one it
// tries ExplainFindingByPublicIDQuery first and falls back to this query
// only when that finds nothing.
var ExplainFindingQuery = supplyChainImpactReadModeSQL(buildExplainSupplyChainImpactFindingQuery(
	"",
	`
  WHERE $2 = ''
     OR fact_id = $2
     OR finding_id = $2
     OR canonical_key = $2
     OR canonical_key IN (
          SELECT canonical_key
          FROM authorized_source_candidates AS identity_candidate
          WHERE identity_candidate.fact_id = $2
        )`,
), false)

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
         ` + supplyChainImpactProtectedSourceCanonicalKeySQL + ` AS canonical_key
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
` + supplyChainImpactProtectedSourceJoinsSQL + `
  WHERE fact.fact_kind = $1
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND fact.scope_id <> '` + supplyChainImpactOperatorSuppressionScopeID + `'
` + supplyChainImpactProtectedSourcePredicateSQL("$14", "$15", "$16", "$17", "$18", "$19", "$12", "$13") + `
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
      OR finding_class.source_class = 'derived'
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
	ctx context.Context,
	filter ExplanationFilter,
	now func() time.Time,
) []any {
	args := []any{
		FindingFactKind,
		filter.FindingID,
		filter.AdvisoryID,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.WorkloadID,
		filter.ServiceID,
		filter.ImageRef,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
		SuppressionReadAt(now),
	}
	return append(args, supplyChainImpactCallerAuthorizationArgs(ctx)...)
}

const explainSupplyChainImpactAuthorizedEvidenceFactsQuery = `
SELECT fact.fact_id, fact.fact_kind, fact.source_system, fact.source_confidence, fact.observed_at, fact.schema_version, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
LEFT JOIN scanner_image_result_fact_associations AS finding_association
  ON finding_association.finding_fact_id = $1
WHERE fact.fact_id = ANY($2::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (finding_association.finding_fact_id IS NULL OR EXISTS (
    SELECT 1 FROM scanner_image_result_evidence_associations AS evidence_link
    WHERE evidence_link.finding_fact_id = $1
      AND evidence_link.publication_id = finding_association.publication_id
      AND evidence_link.evidence_fact_id = fact.fact_id
      AND ((evidence_link.evidence_class = 'scan_intrinsic'
            AND fact.scope_id = finding_association.scan_scope_id
            AND fact.generation_id = finding_association.scan_generation_id)
        OR (evidence_link.evidence_class = 'advisory'
            AND EXISTS (
              SELECT 1 FROM scanner_image_result_advisory_dependencies AS dependency
              WHERE dependency.publication_id = finding_association.publication_id
                AND dependency.authority_kind = evidence_link.authority_kind
                AND dependency.authority_id = evidence_link.authority_id
                AND dependency.authority_epoch = evidence_link.authority_epoch
                AND fact.scope_id = dependency.advisory_scope_id
                AND fact.generation_id = dependency.advisory_generation_id)))))
ORDER BY fact.fact_id ASC
`

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

// TrimExplanationFilter trims whitespace from every scope-anchor field on
// filter, so a caller-supplied value with leading or trailing space does not
// silently miss an exact-match anchor.
func TrimExplanationFilter(
	filter ExplanationFilter,
) ExplanationFilter {
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
