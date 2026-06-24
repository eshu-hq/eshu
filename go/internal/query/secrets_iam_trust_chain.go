// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const secretsIAMIdentityTrustChainFactKind = "reducer_secrets_iam_identity_trust_chain"

// SecretsIAMIdentityTrustChainStore reads reducer-owned secrets/IAM identity
// trust chains (issue #25). The store is the read half of the reducer producer
// (reducer_secrets_iam_identity_trust_chain facts); it writes nothing and
// projects no graph edges. Rows carry stable fingerprints and source-safe
// object IDs only, preserving the metadata-only contract: raw IAM role ARNs,
// ServiceAccount names, namespaces, Vault role names, and paths are never read.
type SecretsIAMIdentityTrustChainStore interface {
	ListSecretsIAMIdentityTrustChains(context.Context, SecretsIAMIdentityTrustChainFilter) ([]SecretsIAMIdentityTrustChainRow, error)
}

// SecretsIAMIdentityTrustChainFilter bounds chain reads to a concrete reducer
// scope, chain, workload object, ServiceAccount join key, IAM role fingerprint,
// or chain state. At least one anchor is required so a read never scans the
// whole fact store.
type SecretsIAMIdentityTrustChainFilter struct {
	ScopeID               string
	ChainID               string
	WorkloadObjectID      string
	ServiceAccountJoinKey string
	IAMRoleFingerprint    string
	State                 string
	AfterChainID          string
	Limit                 int
}

// SecretsIAMIdentityTrustChainRow is one durable identity trust-chain fact. The
// fields mirror the reducer payload written by the secrets/IAM trust-chain
// writer; fingerprints, join keys, states, and evidence IDs only.
type SecretsIAMIdentityTrustChainRow struct {
	ChainID               string
	State                 string
	Confidence            string
	ServiceAccountJoinKey string
	WorkloadObjectID      string
	WorkloadKind          string
	IAMRoleFingerprint    string
	VaultRoleJoinKey      string
	VaultMountJoinKey     string
	VaultPolicyJoinKeys   []string
	EvidenceFactIDs       []string
	MissingEvidence       []string
	SourceScopes          []string
	SourceGenerations     []string
}

type secretsIAMIdentityTrustChainQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresSecretsIAMIdentityTrustChainStore reads active identity trust-chain
// facts from Postgres using bounded payload predicates against the shared
// active-fact read model.
type PostgresSecretsIAMIdentityTrustChainStore struct {
	DB secretsIAMIdentityTrustChainQueryer
}

// NewPostgresSecretsIAMIdentityTrustChainStore creates the Postgres-backed
// secrets/IAM identity trust-chain read model.
func NewPostgresSecretsIAMIdentityTrustChainStore(
	db secretsIAMIdentityTrustChainQueryer,
) PostgresSecretsIAMIdentityTrustChainStore {
	return PostgresSecretsIAMIdentityTrustChainStore{DB: db}
}

// ListSecretsIAMIdentityTrustChains returns one bounded page of active reducer
// identity trust-chain facts. It requires a concrete scope anchor and a bounded
// limit, and orders by fact_id so after_chain_id pagination is deterministic.
func (s PostgresSecretsIAMIdentityTrustChainStore) ListSecretsIAMIdentityTrustChains(
	ctx context.Context,
	filter SecretsIAMIdentityTrustChainFilter,
) ([]SecretsIAMIdentityTrustChainRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("secrets/IAM identity trust-chain database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, chain_id, workload_object_id, service_account_join_key, or iam_role_fingerprint is required")
	}
	if filter.Limit <= 0 || filter.Limit > secretsIAMTrustChainMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", secretsIAMTrustChainMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listSecretsIAMIdentityTrustChainsQuery,
		secretsIAMIdentityTrustChainFactKind,
		filter.ScopeID,
		filter.ChainID,
		filter.WorkloadObjectID,
		filter.ServiceAccountJoinKey,
		filter.IAMRoleFingerprint,
		filter.State,
		filter.AfterChainID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list secrets/IAM identity trust chains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SecretsIAMIdentityTrustChainRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list secrets/IAM identity trust chains: %w", err)
		}
		row, err := decodeSecretsIAMIdentityTrustChainRow(factID, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list secrets/IAM identity trust chains: %w", err)
	}
	return out, nil
}

const listSecretsIAMIdentityTrustChainsQuery = `
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
  AND ($4 = '' OR fact.payload->>'workload_object_id' = $4)
  AND ($5 = '' OR fact.payload->>'service_account_join_key' = $5)
  AND ($6 = '' OR fact.payload->>'iam_role_fingerprint' = $6)
  AND ($7 = '' OR fact.payload->>'state' = $7)
  AND ($8 = '' OR fact.fact_id > $8)
ORDER BY fact.fact_id ASC
LIMIT $9
`

func (f SecretsIAMIdentityTrustChainFilter) hasScope() bool {
	return f.ScopeID != "" ||
		f.ChainID != "" ||
		f.WorkloadObjectID != "" ||
		f.ServiceAccountJoinKey != "" ||
		f.IAMRoleFingerprint != ""
}

func decodeSecretsIAMIdentityTrustChainRow(
	factID string,
	payloadBytes []byte,
) (SecretsIAMIdentityTrustChainRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return SecretsIAMIdentityTrustChainRow{}, fmt.Errorf("decode secrets/IAM identity trust chain: %w", err)
	}
	return SecretsIAMIdentityTrustChainRow{
		ChainID:               factID,
		State:                 StringVal(payload, "state"),
		Confidence:            StringVal(payload, "confidence"),
		ServiceAccountJoinKey: StringVal(payload, "service_account_join_key"),
		WorkloadObjectID:      StringVal(payload, "workload_object_id"),
		WorkloadKind:          StringVal(payload, "workload_kind"),
		IAMRoleFingerprint:    StringVal(payload, "iam_role_fingerprint"),
		VaultRoleJoinKey:      StringVal(payload, "vault_role_join_key"),
		VaultMountJoinKey:     StringVal(payload, "vault_mount_join_key"),
		VaultPolicyJoinKeys:   StringSliceVal(payload, "vault_policy_join_keys"),
		EvidenceFactIDs:       StringSliceVal(payload, "evidence_fact_ids"),
		MissingEvidence:       StringSliceVal(payload, "missing_evidence"),
		SourceScopes:          StringSliceVal(payload, "source_scopes"),
		SourceGenerations:     StringSliceVal(payload, "source_generations"),
	}, nil
}
