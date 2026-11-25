// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Config-leg producer-activation linkage (#11392). The drift evidence joins
// three legs: the AWS generation's own resources, the active
// terraform_state_resource facts by ARN, and the config rows of the commit
// that owns each state backend. The #7635 index covered the state leg only:
// a config-only activation (a repo snapshot carrying terraform_backends
// parser facts, moving the anchor the drift reader resolves) retired
// inapplicable and never reopened drift.
//
// The config arm of producerEvidenceCoreSQL (in
// ingestion_producer_activation_sql.go) mirrors the ownership reader's own
// predicate in listTerraformBackendCanonicalRowsQuery
// (go/internal/storage/postgres/tfstate/backend_canonical.go): file/git
// facts with a non-empty parsed_file_data.terraform_backends array. No
// tombstone filter, because neither reader has one and tombstone payloads
// are empty, so a retraction cannot match the array-length predicate
// anyway; a backend removal that leaves no live declaration owes nothing
// and stays audit-bounded, the same residual as every other missed
// ownership move.
//
// The linkage is a two-hop with the same intersection semantics as the
// state leg: the owed config commit names owned backend kinds, the owned
// state scopes are the active state_snapshot scopes of those kinds, and
// only drift items whose own-generation ARNs intersect the owned states'
// active ARNs reopen. Ownership stops at the backend kind rather than the
// exact (kind, locator) pair on purpose: the locator is derived per kind in
// Go (interpolation, defaults, local-path resolution) with no exact SQL
// mirror, and the ARN intersection below is the exact bound, so a coarser
// ownership hop costs at most a wasted idempotent wake-up, never a wrong
// reopen. The reducer re-run is the authority, as with the looser
// dockerfile mirror documented in ingestion_producer_activation_sql.go.
//
// Cost: one settle spends one config-kinds probe plus one config listing,
// both (scope_id, generation_id[, fact_kind])-anchored, inside the existing
// settle transaction. The per-activation budget is four probes (evidence,
// OCI keys, drift ARNs, config kinds) plus four listings (two OCI families,
// drift, config), pinned by
// TestProducerSettleFactProbeBudgetHermetic. Bulk import stays O(keys +
// dependents): the kinds probe reads one generation, the listing probes the
// floored candidates' scopes only.

// producerOwedConfigKindsQuery reads the owed generation's distinct backend
// kinds once per settle, for the same rescan reason as
// producerOwedDriftARNsQuery. BackendKind is the parser row's literal label
// (go/internal/parser/hcl/terraform_backend.go); blank entries match
// nothing downstream and are dropped here.
const producerOwedConfigKindsQuery = `
SELECT DISTINCT btrim(backend.value->>'backend_kind')
FROM fact_records AS config_fact,
     jsonb_array_elements(CASE
       WHEN jsonb_typeof(config_fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
       THEN config_fact.payload->'parsed_file_data'->'terraform_backends'
       ELSE '[]'::jsonb END) AS backend(value)
WHERE config_fact.scope_id = $1
  AND config_fact.generation_id = $2
  AND config_fact.fact_kind = 'file'
  AND config_fact.source_system = 'git'
  AND CASE
        WHEN jsonb_typeof(config_fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
        THEN jsonb_array_length(config_fact.payload->'parsed_file_data'->'terraform_backends')
        ELSE 0 END > 0
  AND btrim(COALESCE(backend.value->>'backend_kind', '')) <> ''
`

// listProducerOwedConfigKinds reads the owed generation's distinct config
// backend kinds in one probe. It returns an empty (non-nil) slice when the
// generation carries none, so the listing matches nothing.
func listProducerOwedConfigKinds(ctx context.Context, queryer db.Queryer, scopeID, generationID string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, producerOwedConfigKindsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list producer owed config kinds: %w", err)
	}
	defer func() { _ = rows.Close() }()
	kinds := []string{}
	for rows.Next() {
		var kind sql.NullString
		if err := rows.Scan(&kind); err != nil {
			return nil, fmt.Errorf("list producer owed config kinds: scan: %w", err)
		}
		if kind.Valid {
			kinds = append(kinds, kind.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list producer owed config kinds: %w", err)
	}
	return kinds, nil
}

// producerConfigLinkageConjunct narrows the shipped correlation listing to
// the drift items the owed config commit could change: floored succeeded
// drift items, completed before the obligation was owed ($2), whose own
// generation holds an aws_resource ARN that an ACTIVE terraform_state
// resource of an owned-kind state scope ($3, fetched once by
// producerOwedConfigKindsQuery) also holds. The state hop mirrors the
// drift reader's join (active generation only, no tombstone filter on
// either side because the reader has none); the LIKE guard keeps the
// kind-segment match to state_snapshot scopes. The $2 staleness bound
// carries the same NTP-skew assumption documented on
// producerOCILinkageConjunct in ingestion_producer_activation_sql.go.
const producerConfigLinkageConjunct = `
  AND work.updated_at < $2::timestamptz
  AND EXISTS (
    SELECT 1
    FROM fact_records AS aws_fact
    WHERE aws_fact.scope_id = work.scope_id
      AND aws_fact.generation_id = work.generation_id
      AND aws_fact.fact_kind = 'aws_resource'
      AND btrim(COALESCE(aws_fact.payload->>'arn', '')) <> ''
      AND EXISTS (
        SELECT 1
        FROM fact_records AS state_fact
        JOIN ingestion_scopes AS state_scope
          ON state_scope.scope_id = state_fact.scope_id
         AND state_scope.active_generation_id = state_fact.generation_id
        WHERE state_fact.scope_id LIKE 'state_snapshot:%'
          AND state_fact.fact_kind = 'terraform_state_resource'
          AND btrim(COALESCE(state_fact.payload->'attributes'->>'arn', '')) <> ''
          AND state_fact.payload->'attributes'->>'arn' = aws_fact.payload->>'arn'
          AND split_part(state_fact.scope_id, ':', 2) = ANY($3::text[])
      )
  )
`

// listProducerDependentDriftItemsByConfigQuery is the shipped correlation
// reopen listing AND the config two-hop linkage + staleness conjuncts. The
// domain parameter ($1) is always aws_cloud_runtime_drift; $2 is the
// owed-at bound and $3 the owed backend kinds. The derivation keeps the
// shipped replay floor and failed-generation exclusion.
var listProducerDependentDriftItemsByConfigQuery = deriveQueryAtMarker(
	listSucceededReducerWorkItemsByDomainQuery,
	correlationReopenStageMarker,
	producerConfigLinkageConjunct,
)
