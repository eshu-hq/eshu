// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const secretsIAMPostureSummaryCapability = "secrets_iam.posture_summary.read"

// SecretsIAMBucketCount is one grouped count in a posture summary: a bucket
// label (a state, risk type, severity, or gap type) and how many reducer-owned
// rows fall in it.
type SecretsIAMBucketCount struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

// SecretsIAMPostureSummary is a bounded, scope-anchored rollup of the four
// secrets/IAM reducer read models plus the S3 external-principal grant graph
// truth, for dashboards. It is provenance-only counts; it exposes no
// fingerprints, paths, or evidence — only low-cardinality bucket labels and
// totals.
type SecretsIAMPostureSummary struct {
	IdentityTrustChainsByState      []SecretsIAMBucketCount `json:"identity_trust_chains_by_state"`
	PrivilegeObservationsByRiskType []SecretsIAMBucketCount `json:"privilege_observations_by_risk_type"`
	PrivilegeObservationsBySeverity []SecretsIAMBucketCount `json:"privilege_observations_by_severity"`
	SecretAccessPathsByState        []SecretsIAMBucketCount `json:"secret_access_paths_by_state"`
	PostureGapsByGapType            []SecretsIAMBucketCount `json:"posture_gaps_by_gap_type"`
	// S3ExternalPrincipalGrantPosture is the issue-#5643 grant section read
	// from the canonical GRANTS_ACCESS_TO edges (the
	// s3_external_principal_grant fact family's read surface per
	// specs/fact-kind-registry.v1.yaml). Populated by the handler from its
	// GrantPosture store, not by the Postgres summary store; omitted when no
	// graph reader is wired.
	S3ExternalPrincipalGrantPosture *SecretsIAMGrantPosture `json:"s3_external_principal_grant_posture,omitempty"`
}

// SecretsIAMPostureSummaryStore reads grouped counts over the secrets/IAM
// reducer read models for one scope.
type SecretsIAMPostureSummaryStore interface {
	SummarizeSecretsIAMPosture(ctx context.Context, scopeID string) (SecretsIAMPostureSummary, error)
}

// PostgresSecretsIAMPostureSummaryStore computes the posture summary with
// bounded, scope-anchored GROUP BY queries against the active-fact read model.
type PostgresSecretsIAMPostureSummaryStore struct {
	DB secretsIAMReadQueryer
}

// NewPostgresSecretsIAMPostureSummaryStore creates the Postgres-backed posture
// summary read model.
func NewPostgresSecretsIAMPostureSummaryStore(db secretsIAMReadQueryer) PostgresSecretsIAMPostureSummaryStore {
	return PostgresSecretsIAMPostureSummaryStore{DB: db}
}

// SummarizeSecretsIAMPosture returns grouped counts for one reducer scope. A
// scope anchor is required so the rollup never scans the whole fact store.
func (s PostgresSecretsIAMPostureSummaryStore) SummarizeSecretsIAMPosture(
	ctx context.Context,
	scopeID string,
) (SecretsIAMPostureSummary, error) {
	if s.DB == nil {
		return SecretsIAMPostureSummary{}, fmt.Errorf("secrets/IAM posture summary database is required")
	}
	if scopeID == "" {
		return SecretsIAMPostureSummary{}, fmt.Errorf("scope_id is required")
	}

	var summary SecretsIAMPostureSummary
	var err error
	if summary.IdentityTrustChainsByState, err = s.bucketCounts(ctx, secretsIAMIdentityTrustChainFactKind, "state", scopeID); err != nil {
		return SecretsIAMPostureSummary{}, err
	}
	if summary.PrivilegeObservationsByRiskType, err = s.bucketCounts(ctx, secretsIAMPrivilegePostureObservationFactKind, "risk_type", scopeID); err != nil {
		return SecretsIAMPostureSummary{}, err
	}
	if summary.PrivilegeObservationsBySeverity, err = s.bucketCounts(ctx, secretsIAMPrivilegePostureObservationFactKind, "severity", scopeID); err != nil {
		return SecretsIAMPostureSummary{}, err
	}
	if summary.SecretAccessPathsByState, err = s.bucketCounts(ctx, secretsIAMSecretAccessPathFactKind, "state", scopeID); err != nil {
		return SecretsIAMPostureSummary{}, err
	}
	if summary.PostureGapsByGapType, err = s.bucketCounts(ctx, secretsIAMPostureGapFactKind, "gap_type", scopeID); err != nil {
		return SecretsIAMPostureSummary{}, err
	}
	return summary, nil
}

// secretsIAMSummaryBucketFields is the closed set of payload fields the summary
// may group by. Restricting the interpolated column to this allow-list keeps
// the GROUP BY free of any caller-influenced SQL.
var secretsIAMSummaryBucketFields = map[string]struct{}{
	"state":     {},
	"risk_type": {},
	"severity":  {},
	"gap_type":  {},
}

func (s PostgresSecretsIAMPostureSummaryStore) bucketCounts(
	ctx context.Context,
	factKind string,
	bucketField string,
	scopeID string,
) ([]SecretsIAMBucketCount, error) {
	if _, ok := secretsIAMSummaryBucketFields[bucketField]; !ok {
		return nil, fmt.Errorf("unsupported summary bucket field %q", bucketField)
	}
	query := fmt.Sprintf(secretsIAMPostureSummaryQueryTemplate, bucketField)
	rows, err := s.DB.QueryContext(ctx, query, factKind, scopeID)
	if err != nil {
		return nil, fmt.Errorf("summarize secrets/IAM posture: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SecretsIAMBucketCount
	for rows.Next() {
		var bucket string
		// COUNT(*) is int64 in Postgres; scan into int64 (consistent with the
		// repo's other aggregate queries and safe on 32-bit) and narrow on use.
		var count int64
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, fmt.Errorf("summarize secrets/IAM posture: %w", err)
		}
		out = append(out, SecretsIAMBucketCount{Bucket: bucket, Count: int(count)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize secrets/IAM posture: %w", err)
	}
	return out, nil
}

// secretsIAMPostureSummaryQueryTemplate groups active reducer facts for one
// scope by a bucket field. The %s is an allow-listed payload column (never
// caller input); fact kind and scope are bound parameters.
// #nosec G101 -- SQL template whose const name contains "Secrets"/"IAM"; the value is a parameterized query template (the %s is an allowlist-validated payload column name, not a credential literal)
const secretsIAMPostureSummaryQueryTemplate = `
SELECT COALESCE(NULLIF(fact.payload->>'%s', ''), 'unknown') AS bucket, COUNT(*) AS bucket_count
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
  AND fact.scope_id = $2
GROUP BY bucket
ORDER BY bucket ASC
`

// summary serves the bounded secrets/IAM posture summary. It is a method on the
// shared SecretsIAMHandler (registered by its Mount).
func (h *SecretsIAMHandler) summary(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMPostureSummary,
		"GET /api/v0/secrets-iam/posture-summary",
		secretsIAMPostureSummaryCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), secretsIAMPostureSummaryCapability) {
		WriteContractError(w, r, http.StatusNotImplemented,
			"secrets/IAM posture summary requires the Postgres reducer read model",
			ErrorCodeUnsupportedCapability, secretsIAMPostureSummaryCapability,
			h.profile(), requiredProfile(secretsIAMPostureSummaryCapability))
		return
	}
	scopeID := QueryParam(r, "scope_id")
	if scopeID == "" {
		WriteError(w, http.StatusBadRequest, "scope_id is required")
		return
	}
	if !authorizeSecretsIAMScopedScope(w, r, scopeID) {
		return
	}
	if h.Summary == nil {
		WriteContractError(w, r, http.StatusServiceUnavailable,
			"secrets/IAM posture summary requires the Postgres reducer read model",
			ErrorCodeBackendUnavailable, secretsIAMPostureSummaryCapability,
			h.profile(), requiredProfile(secretsIAMPostureSummaryCapability))
		return
	}

	summary, err := h.Summary.SummarizeSecretsIAMPosture(r.Context(), scopeID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.GrantPosture != nil {
		grantPosture, err := h.GrantPosture.SummarizeS3ExternalPrincipalGrantPosture(r.Context(), scopeID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		summary.S3ExternalPrincipalGrantPosture = &grantPosture
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"scope_id": scopeID,
		"summary":  summary,
	}, BuildTruthEnvelope(
		h.profile(), secretsIAMPostureSummaryCapability, TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM read models as grouped counts by state, risk type, severity, and gap type, plus S3 external-principal grant counts read from the canonical GRANTS_ACCESS_TO graph edges; provenance-only rollup, no fingerprints or evidence exposed",
	))
}
