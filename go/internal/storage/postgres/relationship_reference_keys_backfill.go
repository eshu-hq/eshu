// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// LookupKeyBackfill rebuilds the #10875 sidecars for facts written before
// the version-1 writer shipped: version-1
// relationship_reference_candidate_keys rows (family and marker columns) and
// security_alert_reconciliation_keys rows. It calls the same refresh funcs
// as the save path, so a rebuilt row is byte-identical to a row the writer
// would have written for the same envelope.
//
// The rebuild is bounded and resumable: scopes are processed in scope_id
// order, up to scopesPerPass per Run call, one page transaction at a time,
// and every page upserts idempotently. Rerunning Run continues where the
// previous run stopped (scopes with no missing rows are skipped by the
// resume probe) and a second full run is a no-op. Per-row keys_version
// (relationship) and the ready marker (security) let the readers trust
// partial progress immediately: versioned rows take the keyed arms while
// unrebuilt facts keep the legacy text arms verbatim.
type LookupKeyBackfill struct{}

// Run rebuilds at most scopesPerPass scopes with missing sidecar rows,
// paging pageSize facts per transaction. It reports completed=false when
// more scopes remain, so the caller loops until completed. Progress and
// rows-per-fact land in the log; a probe failure fails the run it observes.
func (LookupKeyBackfill) Run(ctx context.Context, database db.ExecQueryer, scopesPerPass, pageSize int) (completed bool, err error) {
	if scopesPerPass <= 0 {
		scopesPerPass = 25
	}
	if pageSize <= 0 {
		pageSize = 500
	}
	beginner, ok := database.(db.Beginner)
	if !ok {
		return false, fmt.Errorf("lookup key backfill requires a transaction beginner")
	}
	scopes, err := lookupKeyBackfillScopes(ctx, database, scopesPerPass)
	if err != nil {
		return false, err
	}
	if len(scopes) == 0 {
		return true, nil
	}
	for _, scopeID := range scopes {
		written, facts, err := rebuildLookupKeyScope(ctx, beginner, scopeID, pageSize)
		if err != nil {
			return false, err
		}
		missing, err := countLookupKeyMissingFacts(ctx, database, scopeID)
		if err != nil {
			return false, err
		}
		log.Printf("event_name=lookup_keys.backfill.scope scope_id=%q facts=%d key_rows=%d missing=%d", scopeID, facts, written, missing)
	}
	return len(scopes) < scopesPerPass, nil
}

// MarkSecurityAlertReconciliationKeysReady records the backfill completion
// marker the keyed security-alert reader requires (issue #10875). Call it
// only after a completed Run pass AND the orphan census reads zero for the
// rebuilt generations: until then the legacy six-arm query runs verbatim so
// a partially backfilled table can never under-select. The marker insert is
// idempotent.
func MarkSecurityAlertReconciliationKeysReady(ctx context.Context, database db.ExecQueryer) error {
	if _, err := database.ExecContext(ctx, `INSERT INTO security_alert_reconciliation_key_backfill_markers (marker_name) VALUES ($1) ON CONFLICT (marker_name) DO NOTHING`, SecurityAlertReconciliationKeyBackfillMarker); err != nil {
		return fmt.Errorf("mark security alert reconciliation keys ready: %w", err)
	}
	return nil
}

// securityAlertKeyablePredicateSQL is true when a security-kind fact can
// yield at least one sidecar row under securityAlertReconciliationKeyRows,
// mirroring its extraction exactly: a scalar id field yields unless it is
// null, missing, an object, an array, or an empty string; an array field
// yields on a non-empty string element or a non-empty object key. Provably
// keyless facts are excluded from scope selection and the missing probe so
// both converge once every keyable fact has keys (issue #10875 review);
// the safe direction is preserved because an excluded fact can never gain
// a key row from a rebuild.
const securityAlertKeyablePredicateSQL = `
(
  (jsonb_typeof(fact.payload -> 'repository_id') = 'string' AND fact.payload ->> 'repository_id' <> '')
  OR jsonb_typeof(fact.payload -> 'repository_id') IN ('number', 'boolean')
  OR (jsonb_typeof(fact.payload -> 'package_id') = 'string' AND fact.payload ->> 'package_id' <> '')
  OR jsonb_typeof(fact.payload -> 'package_id') IN ('number', 'boolean')
  OR (jsonb_typeof(fact.payload -> 'cve_id') = 'string' AND fact.payload ->> 'cve_id' <> '')
  OR jsonb_typeof(fact.payload -> 'cve_id') IN ('number', 'boolean')
  OR (jsonb_typeof(fact.payload -> 'advisory_id') = 'string' AND fact.payload ->> 'advisory_id' <> '')
  OR jsonb_typeof(fact.payload -> 'advisory_id') IN ('number', 'boolean')
  OR (jsonb_typeof(fact.payload -> 'cve_ids') = 'array' AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(fact.payload -> 'cve_ids') AS e
    WHERE jsonb_typeof(e.value) = 'string' AND e.value #>> '{}' <> ''))
  OR (jsonb_typeof(fact.payload -> 'cve_ids') = 'object' AND EXISTS (
    SELECT 1 FROM jsonb_object_keys(fact.payload -> 'cve_ids') AS k
    WHERE k <> ''))
  OR (jsonb_typeof(fact.payload -> 'ghsa_ids') = 'array' AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(fact.payload -> 'ghsa_ids') AS e
    WHERE jsonb_typeof(e.value) = 'string' AND e.value #>> '{}' <> ''))
  OR (jsonb_typeof(fact.payload -> 'ghsa_ids') = 'object' AND EXISTS (
    SELECT 1 FROM jsonb_object_keys(fact.payload -> 'ghsa_ids') AS k
    WHERE k <> ''))
)
`

const lookupKeyBackfillScopesSQL = `
SELECT DISTINCT fact.scope_id
FROM fact_records AS fact
WHERE fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship', 'security_alert.repository_alert', 'reducer_package_consumption_correlation')
  AND fact.is_tombstone = FALSE
  AND (
    (
      fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND NOT EXISTS (
        SELECT 1 FROM relationship_reference_candidate_keys AS ref
        WHERE ref.fact_id = fact.fact_id
          AND ref.scope_id = fact.scope_id
          AND ref.generation_id = fact.generation_id
          AND ref.keys_version >= 1
      )
    )
    OR (
      fact.fact_kind IN ('security_alert.repository_alert', 'reducer_package_consumption_correlation')
      AND NOT EXISTS (
        SELECT 1 FROM security_alert_reconciliation_keys AS keys
        WHERE keys.fact_id = fact.fact_id
          AND keys.scope_id = fact.scope_id
          AND keys.generation_id = fact.generation_id
      )
      AND ` + securityAlertKeyablePredicateSQL + `
    )
  )
ORDER BY fact.scope_id
LIMIT $1
`

func lookupKeyBackfillScopes(ctx context.Context, queryer db.Queryer, limit int) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, lookupKeyBackfillScopesSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("list lookup key backfill scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan lookup key backfill scope: %w", err)
		}
		scopes = append(scopes, scopeID)
	}
	return scopes, rows.Err()
}

const lookupKeyBackfillPageSQL = `
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
WHERE fact.scope_id = $1
  AND fact.fact_id > $2
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship', 'security_alert.repository_alert', 'reducer_package_consumption_correlation')
  AND fact.is_tombstone = FALSE
ORDER BY fact.fact_id ASC
LIMIT $3
`

// lookupKeyBackfillMissingSQL counts eligible facts still missing sidecar
// rows on either side: relationship facts without version-1 rows, and
// keyable security-alert facts without key rows. The kind lists mirror
// lookupKeyBackfillPageSQL above and securityAlertReconciliationFactKinds;
// provably keyless security facts are excluded by
// securityAlertKeyablePredicateSQL (same predicate as scope selection), so
// the count converges to zero once every keyable fact has keys.
const lookupKeyBackfillMissingSQL = `
SELECT count(*)
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.is_tombstone = FALSE
  AND (
    (
      fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND NOT EXISTS (
        SELECT 1 FROM relationship_reference_candidate_keys AS ref
        WHERE ref.fact_id = fact.fact_id
          AND ref.scope_id = fact.scope_id
          AND ref.generation_id = fact.generation_id
          AND ref.keys_version >= 1
      )
    ) OR (
      fact.fact_kind IN ('security_alert.repository_alert', 'reducer_package_consumption_correlation')
      AND NOT EXISTS (
        SELECT 1 FROM security_alert_reconciliation_keys AS keys
        WHERE keys.fact_id = fact.fact_id
          AND keys.scope_id = fact.scope_id
          AND keys.generation_id = fact.generation_id
      )
      AND ` + securityAlertKeyablePredicateSQL + `
    )
  )
`

// rebuildLookupKeyScope repages one scope's eligible facts, refreshing both
// sidecars per page in one transaction, and reports key rows written and
// facts paged (rows-per-fact save-cost evidence, issue #10875).
func rebuildLookupKeyScope(ctx context.Context, beginner db.Beginner, scopeID string, pageSize int) (written, facts int64, err error) {
	cursor := ""
	for {
		tx, err := beginner.Begin(ctx)
		if err != nil {
			return written, facts, fmt.Errorf("begin lookup key backfill page: %w", err)
		}
		page, last, done, err := loadLookupKeyBackfillPage(ctx, tx, scopeID, cursor, pageSize)
		if err != nil {
			_ = tx.Rollback()
			return written, facts, err
		}
		if err := refreshRelationshipReferenceCandidateKeys(ctx, tx, page); err != nil {
			_ = tx.Rollback()
			return written, facts, err
		}
		before := len(securityAlertReconciliationKeyRows(page))
		if err := refreshSecurityAlertReconciliationKeys(ctx, tx, page); err != nil {
			_ = tx.Rollback()
			return written, facts, err
		}
		if err := tx.Commit(); err != nil {
			return written, facts, fmt.Errorf("commit lookup key backfill page: %w", err)
		}
		written += int64(len(relationshipReferenceCandidateKeyRows(page)) + before)
		facts += int64(len(page))
		if done {
			return written, facts, nil
		}
		cursor = last
	}
}

func loadLookupKeyBackfillPage(ctx context.Context, queryer db.Queryer, scopeID, cursor string, pageSize int) (page []facts.Envelope, last string, done bool, err error) {
	rows, err := queryer.QueryContext(ctx, lookupKeyBackfillPageSQL, scopeID, cursor, pageSize)
	if err != nil {
		return nil, "", false, fmt.Errorf("load lookup key backfill page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		envelope, err := backfill.ScanFactEnvelope(rows)
		if err != nil {
			return nil, "", false, fmt.Errorf("scan lookup key backfill page: %w", err)
		}
		page = append(page, envelope)
		last = envelope.FactID
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	return page, last, len(page) < pageSize, nil
}

func countLookupKeyMissingFacts(ctx context.Context, queryer db.Queryer, scopeID string) (int64, error) {
	rows, err := queryer.QueryContext(ctx, lookupKeyBackfillMissingSQL, scopeID)
	if err != nil {
		return 0, fmt.Errorf("count lookup key missing facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var missing int64
	if rows.Next() {
		if err := rows.Scan(&missing); err != nil {
			return 0, fmt.Errorf("scan lookup key missing facts: %w", err)
		}
	}
	return missing, rows.Err()
}
