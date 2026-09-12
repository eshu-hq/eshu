// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	secretsIAMPrivilegePostureObservationFactKind = "reducer_secrets_iam_privilege_posture_observation"
	secretsIAMSecretAccessPathFactKind            = "reducer_secrets_iam_secret_access_path"
	secretsIAMPostureGapFactKind                  = "reducer_secrets_iam_posture_gap"
)

// secretsIAMReadQueryer is the bounded read surface shared by the secrets/IAM
// posture stores. It mirrors the active-fact read model used across the query
// package: a single parameterized SELECT with no whole-table scan.
type secretsIAMReadQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// --- Privilege posture observations -----------------------------------------

// IAMPrivilegePostureObservationStore reads reducer-owned privilege
// posture observations: risky broad or partial posture evidence (for example a
// role with external trust and no sts:ExternalId) that must never be promoted
// to an exact path.
type IAMPrivilegePostureObservationStore interface {
	ListSecretsIAMPrivilegePostureObservations(context.Context, IAMPrivilegePostureObservationFilter) ([]IAMPrivilegePostureObservationRow, error)
}

// IAMPrivilegePostureObservationFilter bounds reads to a reducer scope,
// observation, risk type, severity, or state. A scope or observation anchor is
// required so a read never scans the whole fact store.
type IAMPrivilegePostureObservationFilter struct {
	ScopeID            string
	ObservationID      string
	RiskType           string
	Severity           string
	State              string
	AfterObservationID string
	Limit              int
}

// IAMPrivilegePostureObservationRow is one durable privilege posture
// observation fact.
type IAMPrivilegePostureObservationRow struct {
	ObservationID      string
	RiskType           string
	Severity           string
	State              string
	Confidence         string
	SubjectFingerprint string
	Reason             string
	EvidenceFactIDs    []string
}

// PostgresIAMPrivilegePostureObservationStore reads active privilege
// posture observation facts from Postgres using bounded payload predicates.
type PostgresIAMPrivilegePostureObservationStore struct {
	DB secretsIAMReadQueryer
}

// NewPostgresIAMPrivilegePostureObservationStore creates the
// Postgres-backed privilege posture observation read model.
func NewPostgresIAMPrivilegePostureObservationStore(
	db secretsIAMReadQueryer,
) PostgresIAMPrivilegePostureObservationStore {
	return PostgresIAMPrivilegePostureObservationStore{DB: db}
}

// ListSecretsIAMPrivilegePostureObservations returns one bounded page of active
// reducer privilege posture observation facts.
func (s PostgresIAMPrivilegePostureObservationStore) ListSecretsIAMPrivilegePostureObservations(
	ctx context.Context,
	filter IAMPrivilegePostureObservationFilter,
) ([]IAMPrivilegePostureObservationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("secrets/IAM privilege posture observation database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id or observation_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > secretsIAMTrustChainMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", secretsIAMTrustChainMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listSecretsIAMPrivilegePostureObservationsQuery,
		secretsIAMPrivilegePostureObservationFactKind,
		filter.ScopeID,
		filter.ObservationID,
		filter.RiskType,
		filter.Severity,
		filter.State,
		filter.AfterObservationID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list secrets/IAM privilege posture observations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]IAMPrivilegePostureObservationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list secrets/IAM privilege posture observations: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode secrets/IAM privilege posture observation: %w", err)
		}
		out = append(out, IAMPrivilegePostureObservationRow{
			ObservationID:      factID,
			RiskType:           querycontract.StringVal(payload, "risk_type"),
			Severity:           querycontract.StringVal(payload, "severity"),
			State:              querycontract.StringVal(payload, "state"),
			Confidence:         querycontract.StringVal(payload, "confidence"),
			SubjectFingerprint: querycontract.StringVal(payload, "subject_fingerprint"),
			Reason:             querycontract.StringVal(payload, "reason"),
			EvidenceFactIDs:    querycontract.StringSliceVal(payload, "evidence_fact_ids"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list secrets/IAM privilege posture observations: %w", err)
	}
	return out, nil
}

// #nosec G101 -- SQL SELECT whose const name contains "Secrets"/"IAM"; the value is a fully-parameterized query, not a credential literal
const listSecretsIAMPrivilegePostureObservationsQuery = `
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
  AND ($3 = '' OR fact.fact_id = $3)
  AND ($4 = '' OR fact.payload->>'risk_type' = $4)
  AND ($5 = '' OR fact.payload->>'severity' = $5)
  AND ($6 = '' OR fact.payload->>'state' = $6)
  AND ($7 = '' OR fact.fact_id > $7)
ORDER BY fact.fact_id ASC
LIMIT $8
`

func (f IAMPrivilegePostureObservationFilter) hasScope() bool {
	return f.ScopeID != "" || f.ObservationID != ""
}

// --- Secret access paths -----------------------------------------------------

// IAMSecretAccessPathStore reads reducer-owned Vault policy-to-KV
// metadata access paths reachable from an exact identity chain.
type IAMSecretAccessPathStore interface {
	ListSecretsIAMSecretAccessPaths(context.Context, IAMSecretAccessPathFilter) ([]IAMSecretAccessPathRow, error)
}

// IAMSecretAccessPathFilter bounds reads to a reducer scope, path, parent
// chain, Vault mount join key, or state. A scope, chain, or mount anchor is
// required.
type IAMSecretAccessPathFilter struct {
	ScopeID           string
	PathID            string
	ChainID           string
	VaultMountJoinKey string
	State             string
	AfterPathID       string
	Limit             int
}

// IAMSecretAccessPathRow is one durable secret access path fact.
type IAMSecretAccessPathRow struct {
	PathID             string
	ChainID            string
	State              string
	Confidence         string
	KVPathFingerprint  string
	VaultMountJoinKey  string
	VaultPolicyJoinKey string
	Capabilities       []string
	EvidenceFactIDs    []string
}

// PostgresIAMSecretAccessPathStore reads active secret access path facts
// from Postgres using bounded payload predicates.
type PostgresIAMSecretAccessPathStore struct {
	DB secretsIAMReadQueryer
}

// NewPostgresIAMSecretAccessPathStore creates the Postgres-backed secret
// access path read model.
func NewPostgresIAMSecretAccessPathStore(
	db secretsIAMReadQueryer,
) PostgresIAMSecretAccessPathStore {
	return PostgresIAMSecretAccessPathStore{DB: db}
}

// ListSecretsIAMSecretAccessPaths returns one bounded page of active reducer
// secret access path facts.
func (s PostgresIAMSecretAccessPathStore) ListSecretsIAMSecretAccessPaths(
	ctx context.Context,
	filter IAMSecretAccessPathFilter,
) ([]IAMSecretAccessPathRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("secrets/IAM secret access path database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, path_id, chain_id, or vault_mount_join_key is required")
	}
	if filter.Limit <= 0 || filter.Limit > secretsIAMTrustChainMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", secretsIAMTrustChainMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listSecretsIAMSecretAccessPathsQuery,
		secretsIAMSecretAccessPathFactKind,
		filter.ScopeID,
		filter.PathID,
		filter.ChainID,
		filter.VaultMountJoinKey,
		filter.State,
		filter.AfterPathID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list secrets/IAM secret access paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]IAMSecretAccessPathRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list secrets/IAM secret access paths: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode secrets/IAM secret access path: %w", err)
		}
		out = append(out, IAMSecretAccessPathRow{
			PathID:             factID,
			ChainID:            querycontract.StringVal(payload, "chain_id"),
			State:              querycontract.StringVal(payload, "state"),
			Confidence:         querycontract.StringVal(payload, "confidence"),
			KVPathFingerprint:  querycontract.StringVal(payload, "kv_path_fingerprint"),
			VaultMountJoinKey:  querycontract.StringVal(payload, "vault_mount_join_key"),
			VaultPolicyJoinKey: querycontract.StringVal(payload, "vault_policy_join_key"),
			Capabilities:       querycontract.StringSliceVal(payload, "capabilities"),
			EvidenceFactIDs:    querycontract.StringSliceVal(payload, "evidence_fact_ids"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list secrets/IAM secret access paths: %w", err)
	}
	return out, nil
}

// #nosec G101 -- SQL SELECT whose const name contains "Secrets"/"IAM"; the value is a fully-parameterized query, not a credential literal
const listSecretsIAMSecretAccessPathsQuery = `
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
  AND ($3 = '' OR fact.fact_id = $3)
  AND ($4 = '' OR fact.payload->>'chain_id' = $4)
  AND ($5 = '' OR fact.payload->>'vault_mount_join_key' = $5)
  AND ($6 = '' OR fact.payload->>'state' = $6)
  AND ($7 = '' OR fact.fact_id > $7)
ORDER BY fact.fact_id ASC
LIMIT $8
`

func (f IAMSecretAccessPathFilter) hasScope() bool {
	return f.ScopeID != "" || f.PathID != "" || f.ChainID != "" || f.VaultMountJoinKey != ""
}

// --- Posture gaps ------------------------------------------------------------

// IAMPostureGapStore reads reducer-owned posture gaps: missing, stale,
// hidden, or unsupported evidence that prevents exact trust-chain truth.
type IAMPostureGapStore interface {
	ListSecretsIAMPostureGaps(context.Context, IAMPostureGapFilter) ([]IAMPostureGapRow, error)
}

// IAMPostureGapFilter bounds reads to a reducer scope, gap, gap type,
// ServiceAccount join key, or state. A scope, gap, or ServiceAccount anchor is
// required.
type IAMPostureGapFilter struct {
	ScopeID               string
	GapID                 string
	GapType               string
	ServiceAccountJoinKey string
	State                 string
	AfterGapID            string
	Limit                 int
}

// IAMPostureGapRow is one durable posture gap fact.
type IAMPostureGapRow struct {
	GapID                 string
	GapType               string
	State                 string
	Reason                string
	ServiceAccountJoinKey string
	EvidenceFactIDs       []string
	MissingEvidence       []string
	UnsupportedLayers     []string
}

// PostgresIAMPostureGapStore reads active posture gap facts from Postgres
// using bounded payload predicates.
type PostgresIAMPostureGapStore struct {
	DB secretsIAMReadQueryer
}

// NewPostgresIAMPostureGapStore creates the Postgres-backed posture gap
// read model.
func NewPostgresIAMPostureGapStore(
	db secretsIAMReadQueryer,
) PostgresIAMPostureGapStore {
	return PostgresIAMPostureGapStore{DB: db}
}

// ListSecretsIAMPostureGaps returns one bounded page of active reducer posture
// gap facts.
func (s PostgresIAMPostureGapStore) ListSecretsIAMPostureGaps(
	ctx context.Context,
	filter IAMPostureGapFilter,
) ([]IAMPostureGapRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("secrets/IAM posture gap database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, gap_id, or service_account_join_key is required")
	}
	if filter.Limit <= 0 || filter.Limit > secretsIAMTrustChainMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", secretsIAMTrustChainMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listSecretsIAMPostureGapsQuery,
		secretsIAMPostureGapFactKind,
		filter.ScopeID,
		filter.GapID,
		filter.GapType,
		filter.ServiceAccountJoinKey,
		filter.State,
		filter.AfterGapID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list secrets/IAM posture gaps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]IAMPostureGapRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list secrets/IAM posture gaps: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode secrets/IAM posture gap: %w", err)
		}
		out = append(out, IAMPostureGapRow{
			GapID:                 factID,
			GapType:               querycontract.StringVal(payload, "gap_type"),
			State:                 querycontract.StringVal(payload, "state"),
			Reason:                querycontract.StringVal(payload, "reason"),
			ServiceAccountJoinKey: querycontract.StringVal(payload, "service_account_join_key"),
			EvidenceFactIDs:       querycontract.StringSliceVal(payload, "evidence_fact_ids"),
			MissingEvidence:       querycontract.StringSliceVal(payload, "missing_evidence"),
			UnsupportedLayers:     querycontract.StringSliceVal(payload, "unsupported_layers"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list secrets/IAM posture gaps: %w", err)
	}
	return out, nil
}

// #nosec G101 -- SQL SELECT whose const name contains "Secrets"/"IAM"; the value is a fully-parameterized query, not a credential literal
const listSecretsIAMPostureGapsQuery = `
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
  AND ($3 = '' OR fact.fact_id = $3)
  AND ($4 = '' OR fact.payload->>'gap_type' = $4)
  AND ($5 = '' OR fact.payload->>'service_account_join_key' = $5)
  AND ($6 = '' OR fact.payload->>'state' = $6)
  AND ($7 = '' OR fact.fact_id > $7)
ORDER BY fact.fact_id ASC
LIMIT $8
`

func (f IAMPostureGapFilter) hasScope() bool {
	return f.ScopeID != "" || f.GapID != "" || f.ServiceAccountJoinKey != ""
}
