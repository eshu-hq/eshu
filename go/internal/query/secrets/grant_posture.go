// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// secretsIAMGrantPostureReadTimeout bounds the graph aggregate reads behind
// the posture-summary grant section. The reads are five single-aggregate
// statements over the GRANTS_ACCESS_TO edge population (bounded to S3
// bucket-policy external-principal grants), so 10s mirrors the
// codeownersOwnershipReadTimeout budget for bounded dashboard graph reads.
const secretsIAMGrantPostureReadTimeout = 10 * time.Second

// IAMGrantPosture is the bounded S3 external-principal grant section of
// the posture summary (issue #5643). It is aggregate counts over the canonical
// (:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal) edges the
// s3_external_principal_grant_materialization reducer domain writes — the
// materialized graph truth, not the raw fact store, so unsupported grants and
// grants whose source bucket never materialized are truthfully excluded.
// Counts only; no principal identities, ARNs, or bucket names cross the wire,
// preserving the summary's provenance-only contract.
type IAMGrantPosture struct {
	// TotalGrants is derived from the grant_outcome grouping: grant_outcome is
	// a required edge property, so every edge lands in exactly one outcome
	// bucket and the bucket sum equals the edge count.
	TotalGrants            int              `json:"total_grants"`
	GrantsByOutcome        []IAMBucketCount `json:"grants_by_outcome"`
	GrantsByResolutionMode []IAMBucketCount `json:"grants_by_resolution_mode"`
	PublicGrants           int              `json:"public_grants"`
	CrossAccountGrants     int              `json:"cross_account_grants"`
	ServicePrincipalGrants int              `json:"service_principal_grants"`
}

// IAMGrantPostureStore reads the aggregate S3 external-principal grant
// posture for one reducer scope.
type IAMGrantPostureStore interface {
	SummarizeS3ExternalPrincipalGrantPosture(ctx context.Context, scopeID string) (IAMGrantPosture, error)
}

// GraphIAMGrantPostureStore reads the grant posture via the GraphQuery
// port so handler tests can inject a stub that asserts on the Cypher shape and
// parameter bag the production code sends (the GraphPackageRegistryAggregateStore
// precedent).
type GraphIAMGrantPostureStore struct {
	Graph querycontract.GraphQuery
}

// NewGraphIAMGrantPostureStore wires a GraphQuery (Neo4jReader in
// production) into the grant-posture reader.
func NewGraphIAMGrantPostureStore(graph querycontract.GraphQuery) GraphIAMGrantPostureStore {
	return GraphIAMGrantPostureStore{Graph: graph}
}

// Cypher shapes: every read anchors on the canonical GRANTS_ACCESS_TO edge
// pattern and filters on rel.scope_id (the same scope bound the handler's
// #5167 authorization already enforced). Relationship properties carry no
// graph index, so each statement is a relationship-type-bounded scan — the
// GRANTS_ACCESS_TO population is written only by the
// s3_external_principal_grant_materialization reducer domain and is bounded by
// the corpus's S3 bucket-policy external-principal statements (expected
// O(10^2..10^3) edges), with aggregate-only output. The grouped reads use the
// single-grouping-key `RETURN <bucket expr>, count(*)` shape validated by the
// package-registry aggregate store rather than one multi-key grouping
// statement, because multi-key grouped aggregation is not a validated hot-path
// template on the pinned NornicDB binary. Grouping keys are closed
// low-cardinality vocabularies (grant_outcome, resolution_mode), so output
// stays at a handful of rows per statement.
// #nosec G101 -- Cypher graph-query fragment (a MATCH pattern over the
// GRANTS_ACCESS_TO edge), not a credential; scope_id is bound via the
// $scope_id parameter and no request-derived value is concatenated in.
const secretsIAMGrantPostureMatch = `MATCH (:CloudResource)-[rel:GRANTS_ACCESS_TO]->(:ExternalPrincipal)
WHERE rel.scope_id = $scope_id`

// secretsIAMGrantsByOutcomeCypher groups the scope's grant edges by
// grant_outcome. The CASE mirrors the package-registry bucket normalization:
// Cypher coalesce only collapses NULLs, so empty strings are mapped to
// `unknown` explicitly (grant_outcome is required, so this is defensive).
const secretsIAMGrantsByOutcomeCypher = secretsIAMGrantPostureMatch + `
RETURN CASE WHEN rel.grant_outcome IS NULL OR rel.grant_outcome = '' THEN 'unknown' ELSE rel.grant_outcome END AS bucket,
       count(*) AS bucket_count
ORDER BY bucket ASC`

// secretsIAMGrantsByResolutionModeCypher groups the scope's grant edges by
// resolution_mode. resolution_mode is optional on the fact payload, so NULL
// and empty both bucket as `unknown`.
const secretsIAMGrantsByResolutionModeCypher = secretsIAMGrantPostureMatch + `
RETURN CASE WHEN rel.resolution_mode IS NULL OR rel.resolution_mode = '' THEN 'unknown' ELSE rel.resolution_mode END AS bucket,
       count(*) AS bucket_count
ORDER BY bucket ASC`

// secretsIAMGrantFlagFields is the closed set of boolean edge properties the
// flag counts may filter on. Restricting the interpolated property to this
// allow-list keeps the Cypher free of any caller-influenced text (mirrors the
// secretsIAMSummaryBucketFields SQL defense in summary.go).
var secretsIAMGrantFlagFields = map[string]struct{}{
	"is_public":            {},
	"is_cross_account":     {},
	"is_service_principal": {},
}

// secretsIAMGrantFlagCountCypher builds the bounded count statement for one
// allow-listed boolean grant flag.
func secretsIAMGrantFlagCountCypher(flag string) (string, error) {
	if _, ok := secretsIAMGrantFlagFields[flag]; !ok {
		return "", fmt.Errorf("unsupported grant flag %q", flag)
	}
	return secretsIAMGrantPostureMatch + fmt.Sprintf(`
  AND rel.%s = true
RETURN count(*) AS total`, flag), nil
}

// SummarizeS3ExternalPrincipalGrantPosture returns the aggregate grant posture
// for one reducer scope. A scope anchor is required so the read never spans
// tenants, and every statement runs under one shared deadline.
func (s GraphIAMGrantPostureStore) SummarizeS3ExternalPrincipalGrantPosture(
	ctx context.Context,
	scopeID string,
) (IAMGrantPosture, error) {
	if s.Graph == nil {
		return IAMGrantPosture{}, fmt.Errorf("secrets/IAM grant posture graph is required")
	}
	if scopeID == "" {
		return IAMGrantPosture{}, fmt.Errorf("scope_id is required")
	}

	ctx, cancel := context.WithTimeout(ctx, secretsIAMGrantPostureReadTimeout)
	defer cancel()
	params := map[string]any{"scope_id": scopeID}

	var posture IAMGrantPosture
	var err error
	if posture.GrantsByOutcome, err = s.groupedGrantCounts(ctx, secretsIAMGrantsByOutcomeCypher, params); err != nil {
		return IAMGrantPosture{}, err
	}
	for _, bucket := range posture.GrantsByOutcome {
		posture.TotalGrants += bucket.Count
	}
	if posture.GrantsByResolutionMode, err = s.groupedGrantCounts(ctx, secretsIAMGrantsByResolutionModeCypher, params); err != nil {
		return IAMGrantPosture{}, err
	}
	if posture.PublicGrants, err = s.grantFlagCount(ctx, "is_public", params); err != nil {
		return IAMGrantPosture{}, err
	}
	if posture.CrossAccountGrants, err = s.grantFlagCount(ctx, "is_cross_account", params); err != nil {
		return IAMGrantPosture{}, err
	}
	if posture.ServicePrincipalGrants, err = s.grantFlagCount(ctx, "is_service_principal", params); err != nil {
		return IAMGrantPosture{}, err
	}
	return posture, nil
}

func (s GraphIAMGrantPostureStore) groupedGrantCounts(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]IAMBucketCount, error) {
	rows, err := s.Graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("summarize s3 external-principal grant posture: %w", err)
	}
	var out []IAMBucketCount
	for _, row := range rows {
		out = append(out, IAMBucketCount{
			Bucket: querycontract.StringVal(row, "bucket"),
			Count:  querycontract.IntVal(row, "bucket_count"),
		})
	}
	return out, nil
}

func (s GraphIAMGrantPostureStore) grantFlagCount(
	ctx context.Context,
	flag string,
	params map[string]any,
) (int, error) {
	cypher, err := secretsIAMGrantFlagCountCypher(flag)
	if err != nil {
		return 0, err
	}
	rows, err := s.Graph.Run(ctx, cypher, params)
	if err != nil {
		return 0, fmt.Errorf("summarize s3 external-principal grant posture: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return querycontract.IntVal(rows[0], "total"), nil
}
