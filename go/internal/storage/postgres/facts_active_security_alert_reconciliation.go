// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const listActiveSecurityAlertReconciliationFactsArm = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
    'security_alert.repository_alert',
    'reducer_package_consumption_correlation'
)
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'repository_id' = ANY($1::text[])
      OR fact.payload->>'package_id' = ANY($2::text[])
      OR fact.payload->'cve_ids' ?| $3::text[]
      OR fact.payload->'ghsa_ids' ?| $4::text[]
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR fact.payload->>'advisory_id' = ANY($4::text[])
  )
  AND ($5 = '' OR fact.fact_id > $5)
ORDER BY fact.fact_id ASC
LIMIT $6
`

// ListActiveSecurityAlertReconciliationFacts loads active owned dependency and
// reducer impact evidence used to compare provider-reported repository alerts.
// Alert and consumption facts come from fact_records; the supply-chain impact
// findings come from the per-support table (#11198) as impact-kind envelopes
// carrying the verbatim payload. Both arms are one UNION ALL statement with one
// keyset on the fact id, so the order and the paging are the database's own
// ordering of the ids, exactly the single fact read's. The supports arm refuses
// first, with the retryable non-counting
// *supplychaincore.SupplyChainImpactSupportsNotReadyError, until the supports
// backfill is ready: an unready table must not read as "no impact finding",
// which the handler turns into a durable provider_only decision. Every read of
// the handler precedes its one write, so the loader-level refusal defers the
// intent with nothing written.
func (s FactStore) ListActiveSecurityAlertReconciliationFacts(
	ctx context.Context,
	filter securityalert.SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.PackageIDs = cleanStringFilterValues(filter.PackageIDs)
	filter.CVEIDs = cleanStringFilterValues(filter.CVEIDs)
	filter.GHSAIDs = cleanStringFilterValues(filter.GHSAIDs)
	if len(filter.RepositoryIDs) == 0 && len(filter.PackageIDs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.GHSAIDs) == 0 {
		return nil, nil
	}
	if err := requireSupplyChainImpactSupportsReady(ctx, s.database, s.instruments,
		telemetry.SupplyChainImpactSupportsReaderSecurityAlertReconciliation); err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	var loaded []facts.Envelope
	var cursorFactID string
	for {
		page, err := s.listActiveSecurityAlertReconciliationFactsPage(ctx, filter, cursorFactID)
		if err != nil {
			s.instruments.RecordSupplyChainImpactSupportsRead(ctx,
				telemetry.SupplyChainImpactSupportsReaderSecurityAlertReconciliation, telemetry.SupplyChainImpactSupportsReadFailed)
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			s.instruments.RecordSupplyChainImpactSupportsRead(ctx,
				telemetry.SupplyChainImpactSupportsReaderSecurityAlertReconciliation, telemetry.SupplyChainImpactSupportsReadServed)
			return loaded, nil
		}
		cursorFactID = page[len(page)-1].FactID
	}
}

func (s FactStore) listActiveSecurityAlertReconciliationFactsPage(
	ctx context.Context,
	filter securityalert.SecurityAlertReconciliationFactFilter,
	cursorFactID string,
) ([]facts.Envelope, error) {
	// The keyed sidecar path runs only after the backfill marker proves every
	// eligible fact has keys; until then the legacy six-arm query runs
	// verbatim so a partially backfilled table can never under-select
	// (issue #10875).
	query := listActiveSecurityAlertReconciliationFactsQuery
	if ready, err := SecurityAlertReconciliationKeysReady(ctx, s.database); err != nil {
		// Fail open to the legacy query, but say so: a silent fallback
		// hides a broken readiness path behind correct-looking results
		// (issue #10875 review). Only filter-class counts are logged;
		// values are unbounded.
		log.Printf("event_name=security_alert_reconciliation_keys.readiness_probe_failed error=%q repository_filters=%d package_filters=%d cve_filters=%d ghsa_filters=%d",
			err, len(filter.RepositoryIDs), len(filter.PackageIDs), len(filter.CVEIDs), len(filter.GHSAIDs))
	} else if ready {
		query = listActiveSecurityAlertReconciliationFactsKeyedQuery
	}
	rows, err := s.database.QueryContext(
		ctx,
		query,
		filter.RepositoryIDs,
		filter.PackageIDs,
		filter.CVEIDs,
		filter.GHSAIDs,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0,
		len(filter.RepositoryIDs)+len(filter.PackageIDs)+len(filter.CVEIDs)+len(filter.GHSAIDs))
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active security alert reconciliation facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	return loaded, nil
}

// securityAlertReconciliationSupportsArm is the supports half of the loader
// statement (#11198, #10880 PR-B2c). It reads the per-support table in the
// raw-first shape: the supports are selected by their filter first, in the
// statement's MATERIALIZED CTE, and only then joined to the active generation
// (the natural single-WHERE form plans one probe per active scope: 313 to 389 ms
// at 600,000 supports over 6,000 scopes). A support row holds the fact payload
// verbatim, so each arm is the fact arm over the same value: the repository,
// package, cve and advisory columns hold payload->>'repository_id',
// 'package_id', 'cve_id' and 'advisory_id'. The package, cve and advisory arms
// carry the literal <> ” guard so they can use a partial btree on those
// columns. The OR is only indexable when every arm is: with no package_id
// index the planner scans the whole table for every shape (8,000,000 supports:
// 0.9 s warm, about 100 s cold; with a partial package_id btree 0.09 to 0.17 s,
// docs/internal/evidence/10880-b2c/reader_plans.md). The
// legacy payload array arms (payload->'cve_ids' and 'ghsa_ids') are not
// reproduced: the typed finding payload has no such field, so they never matched
// a finding fact (TestSupplyChainImpactFindingPayloadCarriesNoIDArrays pins it),
// and keeping them would make the OR unindexable. The projection pads the fact
// envelope columns a support row does not carry; the reconciliation handler
// reads only the fact id and the payload of an impact envelope
// (extractSecurityAlertImpacts), and the carrier kind is the impact kind.
// supplyChainImpactSupportsPackageIndexPredicate is the partial-index predicate
// of the supports table's package_id btree ((package_id, fact_id) WHERE
// package_id <> ”, migration 345, #10880 PR-B2c). The package arm of the
// reconciliation read repeats it as a literal guard so the planner can prove
// the partial index usable; TestSupplyChainImpactSupportsPackageIndexMatchesTheArm
// derives the arm and the migration from this one constant.
const supplyChainImpactSupportsPackageIndexPredicate = `package_id <> ''`

const securityAlertReconciliationSupportsCTE = `matched AS MATERIALIZED (
    SELECT support.fact_id, support.scope_id, support.generation_id, support.source_confidence, support.payload
    FROM supply_chain_impact_finding_supports AS support
    WHERE (
          support.repository_id = ANY($1::text[])
          OR (support.` + supplyChainImpactSupportsPackageIndexPredicate + ` AND support.package_id = ANY($2::text[]))
          OR (support.cve_id <> '' AND support.cve_id = ANY($3::text[]))
          OR (support.advisory_id <> '' AND support.advisory_id = ANY($4::text[]))
      )
      AND ($5 = '' OR support.fact_id > $5)
)`

const securityAlertReconciliationSupportsArm = `
SELECT
    matched.fact_id,
    matched.scope_id,
    matched.generation_id,
    'reducer_supply_chain_impact_finding'::text AS fact_kind,
    ''::text AS stable_fact_key,
    ''::text AS schema_version,
    ''::text AS collector_kind,
    0::bigint AS fencing_token,
    matched.source_confidence,
    ''::text AS source_system,
    ''::text AS source_fact_key,
    ''::text AS source_uri,
    ''::text AS source_record_id,
    to_timestamp(0) AS observed_at,
    FALSE AS is_tombstone,
    matched.payload
FROM matched
JOIN ingestion_scopes AS scope
  ON scope.scope_id = matched.scope_id
 AND scope.active_generation_id = matched.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = matched.scope_id
 AND generation.generation_id = matched.generation_id
WHERE generation.status = 'active'
ORDER BY matched.fact_id ASC
LIMIT $6
`

// securityAlertReconciliationMergedQuery merges one fact arm (the legacy or the
// keyed fact statement, each ending in its own ORDER BY fact_id and LIMIT $6) with
// the supports arm into one statement: both arms are bounded by the page limit on
// their own, and the outer ORDER BY ... LIMIT takes the page of the union in the
// database's ordering of the ids, the ordering the single fact read returned.
func securityAlertReconciliationMergedQuery(factArm string) string {
	return "WITH " + securityAlertReconciliationSupportsCTE + `
SELECT * FROM (
(` + strings.TrimSpace(factArm) + `)
UNION ALL
(` + strings.TrimSpace(securityAlertReconciliationSupportsArm) + `)
) AS merged
ORDER BY merged.fact_id ASC
LIMIT $6
`
}

// listActiveSecurityAlertReconciliationFactsQuery is the loader statement over
// the legacy six-arm fact arm; the keyed form is
// listActiveSecurityAlertReconciliationFactsKeyedQuery.
var listActiveSecurityAlertReconciliationFactsQuery = securityAlertReconciliationMergedQuery(listActiveSecurityAlertReconciliationFactsArm)
