package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const cicdRunCorrelationFactKind = "reducer_ci_cd_run_correlation"

// CICDRunCorrelationStore reads reducer-owned CI/CD run correlations.
type CICDRunCorrelationStore interface {
	ListCICDRunCorrelations(context.Context, CICDRunCorrelationFilter) ([]CICDRunCorrelationRow, error)
}

// CICDRunCorrelationFilter bounds run-correlation reads to a concrete repo,
// commit, run, artifact digest, environment, or scope.
type CICDRunCorrelationFilter struct {
	ScopeID            string
	RepositoryID       string
	CommitSHA          string
	ProviderRunID      string
	ArtifactDigest     string
	Environment        string
	Outcome            string
	AfterCorrelationID string
	Limit              int
}

// CICDRunCorrelationRow is one durable CI/CD correlation fact decoded from
// the reducer-owned read model.
type CICDRunCorrelationRow struct {
	CorrelationID   string
	Provider        string
	RunID           string
	RunAttempt      string
	RepositoryID    string
	CommitSHA       string
	Environment     string
	ArtifactDigest  string
	ImageRef        string
	Outcome         string
	Reason          string
	ProvenanceOnly  bool
	CanonicalWrites int
	CanonicalTarget string
	CorrelationKind string
	EvidenceFactIDs []string
}

type cicdRunCorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresCICDRunCorrelationStore reads active CI/CD run correlation facts
// from Postgres using bounded payload predicates and a deterministic cursor.
type PostgresCICDRunCorrelationStore struct {
	DB cicdRunCorrelationQueryer
}

// NewPostgresCICDRunCorrelationStore creates the Postgres-backed CI/CD run
// correlation read model.
func NewPostgresCICDRunCorrelationStore(db cicdRunCorrelationQueryer) PostgresCICDRunCorrelationStore {
	return PostgresCICDRunCorrelationStore{DB: db}
}

// ListCICDRunCorrelations returns one bounded page of active reducer CI/CD run
// correlation facts.
func (s PostgresCICDRunCorrelationStore) ListCICDRunCorrelations(
	ctx context.Context,
	filter CICDRunCorrelationFilter,
) ([]CICDRunCorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("ci/cd run correlation database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, repository_id, commit_sha, provider_run_id, artifact_digest, or environment is required")
	}
	if filter.Limit <= 0 || filter.Limit > cicdRunCorrelationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", cicdRunCorrelationMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listCICDRunCorrelationsQuery,
		cicdRunCorrelationFactKind,
		filter.ScopeID,
		filter.RepositoryID,
		filter.CommitSHA,
		filter.ProviderRunID,
		filter.ArtifactDigest,
		filter.Environment,
		filter.Outcome,
		filter.AfterCorrelationID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list ci/cd run correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]CICDRunCorrelationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list ci/cd run correlations: %w", err)
		}
		row, err := decodeCICDRunCorrelationRow(factID, payloadBytes)
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

const listCICDRunCorrelationsQuery = `
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
  AND ($5 = '' OR fact.payload->>'run_id' = $5)
  AND ($6 = '' OR fact.payload->>'artifact_digest' = $6)
  AND ($7 = '' OR fact.payload->>'environment' = $7)
  AND ($8 = '' OR fact.payload->>'outcome' = $8)
  AND ($9 = '' OR fact.fact_id > $9)
ORDER BY fact.fact_id ASC
LIMIT $10
`

func (f CICDRunCorrelationFilter) hasScope() bool {
	return f.ScopeID != "" ||
		f.RepositoryID != "" ||
		f.CommitSHA != "" ||
		f.ProviderRunID != "" ||
		f.ArtifactDigest != "" ||
		f.Environment != ""
}

func decodeCICDRunCorrelationRow(factID string, payloadBytes []byte) (CICDRunCorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return CICDRunCorrelationRow{}, fmt.Errorf("decode ci/cd run correlation: %w", err)
	}
	return CICDRunCorrelationRow{
		CorrelationID:   factID,
		Provider:        StringVal(payload, "provider"),
		RunID:           StringVal(payload, "run_id"),
		RunAttempt:      StringVal(payload, "run_attempt"),
		RepositoryID:    StringVal(payload, "repository_id"),
		CommitSHA:       StringVal(payload, "commit_sha"),
		Environment:     StringVal(payload, "environment"),
		ArtifactDigest:  StringVal(payload, "artifact_digest"),
		ImageRef:        StringVal(payload, "image_ref"),
		Outcome:         StringVal(payload, "outcome"),
		Reason:          StringVal(payload, "reason"),
		ProvenanceOnly:  BoolVal(payload, "provenance_only"),
		CanonicalWrites: IntVal(payload, "canonical_writes"),
		CanonicalTarget: StringVal(payload, "canonical_target"),
		CorrelationKind: StringVal(payload, "correlation_kind"),
		EvidenceFactIDs: StringSliceVal(payload, "evidence_fact_ids"),
	}, nil
}
