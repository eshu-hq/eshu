// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

const cicdRunCorrelationFactKind = "reducer_ci_cd_run_correlation"

type cicdRunCorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresRunCorrelationStore reads active CI/CD run correlation facts
// from Postgres using bounded payload predicates and a deterministic cursor.
type PostgresRunCorrelationStore struct {
	DB cicdRunCorrelationQueryer
}

// NewPostgresRunCorrelationStore creates the Postgres-backed CI/CD run
// correlation read model.
func NewPostgresRunCorrelationStore(db cicdRunCorrelationQueryer) PostgresRunCorrelationStore {
	return PostgresRunCorrelationStore{DB: db}
}

// ListCICDRunCorrelations returns one bounded page of active reducer CI/CD run
// correlation facts.
func (s PostgresRunCorrelationStore) ListCICDRunCorrelations(
	ctx context.Context,
	filter querycontract.CICDRunCorrelationFilter,
) ([]querycontract.CICDRunCorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("ci/cd run correlation database is required")
	}
	if !filter.HasScope() {
		return nil, fmt.Errorf("scope_id, repository_id, commit_sha, provider_run_id, artifact_digest, or environment is required")
	}
	if filter.Limit <= 0 || filter.Limit > cicdRunCorrelationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", cicdRunCorrelationMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listRunCorrelationsQuery,
		cicdRunCorrelationFactKind,
		filter.ScopeID,
		filter.RepositoryID,
		filter.CommitSHA,
		filter.Provider,
		filter.ProviderRunID,
		filter.ArtifactDigest,
		filter.ImageRef,
		filter.Environment,
		filter.Outcome,
		filter.AfterCorrelationID,
		filter.Limit,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list ci/cd run correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]querycontract.CICDRunCorrelationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list ci/cd run correlations: %w", err)
		}
		row, err := decodeRunCorrelationRow(factID, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list ci/cd run correlations: %w", err)
	}
	return out, nil
}

const listRunCorrelationsQuery = `
SELECT fact.fact_id, fact.payload
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
  AND ($2 = '' OR fact.scope_id = $2)
  AND ($3 = '' OR fact.payload->>'repository_id' = $3)
  AND ($4 = '' OR fact.payload->>'commit_sha' = $4)
  AND ($5 = '' OR fact.payload->>'provider' = $5)
  AND ($6 = '' OR fact.payload->>'run_id' = $6)
  AND ($7 = '' OR fact.payload->>'artifact_digest' = $7)
  AND ($8 = '' OR fact.payload->>'image_ref' = $8)
  AND ($9 = '' OR fact.payload->>'environment' = $9)
  AND ($10 = '' OR fact.payload->>'outcome' = $10)
  AND ($11 = '' OR fact.fact_id > $11)
  AND (
    (COALESCE(cardinality($13::text[]), 0) = 0 AND COALESCE(cardinality($14::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($13::text[])
    OR fact.scope_id = ANY($14::text[])
  )
ORDER BY fact.fact_id ASC
LIMIT $12
`

func decodeRunCorrelationRow(factID string, payloadBytes []byte) (querycontract.CICDRunCorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return querycontract.CICDRunCorrelationRow{}, fmt.Errorf("decode ci/cd run correlation: %w", err)
	}
	return querycontract.CICDRunCorrelationRow{
		CorrelationID:       factID,
		Provider:            querycontract.StringVal(payload, "provider"),
		RunID:               querycontract.StringVal(payload, "run_id"),
		RunAttempt:          querycontract.StringVal(payload, "run_attempt"),
		RepositoryID:        querycontract.StringVal(payload, "repository_id"),
		CommitSHA:           querycontract.StringVal(payload, "commit_sha"),
		Environment:         querycontract.StringVal(payload, "environment"),
		EnvironmentEvidence: querycontract.StringVal(payload, "environment_evidence"),
		ArtifactDigest:      querycontract.StringVal(payload, "artifact_digest"),
		ImageRef:            querycontract.StringVal(payload, "image_ref"),
		Outcome:             querycontract.StringVal(payload, "outcome"),
		Reason:              querycontract.StringVal(payload, "reason"),
		ProvenanceOnly:      querycontract.BoolVal(payload, "provenance_only"),
		CanonicalWrites:     querycontract.IntVal(payload, "canonical_writes"),
		CanonicalTarget:     querycontract.StringVal(payload, "canonical_target"),
		CorrelationKind:     querycontract.StringVal(payload, "correlation_kind"),
		EvidenceFactIDs:     querycontract.StringSliceVal(payload, "evidence_fact_ids"),
	}, nil
}
