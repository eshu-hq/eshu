// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const securityAlertReconciliationKeyBatchSize = 500

const columnsPerSecurityAlertReconciliationKeyRow = 5

// SecurityAlertReconciliationKeyBackfillMarker identifies the completed
// security-alert sidecar version the keyed reader requires before it trusts
// the table (the PackageManifestConsumptionKeyBackfillMarker analog, issue
// #10875). The reader uses keys only once this marker is present; until
// then the legacy six-arm query runs verbatim.
const SecurityAlertReconciliationKeyBackfillMarker = "security_alert_reconciliation_keys_v1"

// securityAlertReconciliationFactKinds is the fact-kind surface the keyed
// security-alert reader loads. It must name exactly the kinds in
// listActiveSecurityAlertReconciliationFactsQuery's IN list:
// TestSecurityAlertReconciliationKindFilterMatchesQuery pins both
// directions, because a kind the query reads but the writer skips (or vice
// versa) is a wrong answer in one direction.
var securityAlertReconciliationFactKinds = []string{
	"security_alert.repository_alert",
	"reducer_package_consumption_correlation",
	"reducer_supply_chain_impact_finding",
}

func isSecurityAlertReconciliationFactKind(factKind string) bool {
	for _, kind := range securityAlertReconciliationFactKinds {
		if factKind == kind {
			return true
		}
	}
	return false
}

type securityAlertReconciliationKeyRow struct {
	FactID       string
	ScopeID      string
	GenerationID string
	KeyKind      string
	KeyValue     string
}

// securityAlertReconciliationKeyRows derives one exploded row per legacy
// query predicate value, stored verbatim: no trim, no lowercase. The legacy
// arms compare exact values (payload->>'repository_id' = ANY($1),
// ?| on raw array elements), and cleanStringFilterValues trims only the
// filter side, so the sidecar must keep the fact's bytes untouched for the
// keyed EXISTS to select exactly the legacy set.
func securityAlertReconciliationKeyRows(envelopes []facts.Envelope) []securityAlertReconciliationKeyRow {
	if len(envelopes) == 0 {
		return nil
	}
	rows := make([]securityAlertReconciliationKeyRow, 0, len(envelopes))
	appendKey := func(envelope facts.Envelope, kind, value string) {
		if value == "" {
			return
		}
		rows = append(rows, securityAlertReconciliationKeyRow{
			FactID:       envelope.FactID,
			ScopeID:      envelope.ScopeID,
			GenerationID: envelope.GenerationID,
			KeyKind:      kind,
			KeyValue:     value,
		})
	}
	// appendExistValues mirrors the ?| operator: an array contributes its
	// string elements, an object contributes its key names, and any other
	// shape contributes nothing.
	appendExistValues := func(envelope facts.Envelope, kind string, value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok {
					appendKey(envelope, kind, text)
				}
			}
		case map[string]any:
			for key := range typed {
				appendKey(envelope, kind, key)
			}
		}
	}
	appendScalar := func(envelope facts.Envelope, kind string, value any) {
		if text, ok := value.(string); ok {
			appendKey(envelope, kind, text)
		}
	}
	for _, envelope := range envelopes {
		if envelope.IsTombstone || !isSecurityAlertReconciliationFactKind(envelope.FactKind) {
			continue
		}
		payload := envelope.Payload
		appendScalar(envelope, "repository", payload["repository_id"])
		appendScalar(envelope, "package", payload["package_id"])
		appendExistValues(envelope, "cve", payload["cve_ids"])
		appendScalar(envelope, "cve", payload["cve_id"])
		appendExistValues(envelope, "advisory", payload["ghsa_ids"])
		appendScalar(envelope, "advisory", payload["advisory_id"])
	}
	return rows
}

// refreshSecurityAlertReconciliationKeys replaces the batch's sidecar rows
// with the freshly derived keys in the same transaction that persists the
// facts. The delete covers every accepted fact id (including tombstones and
// retyped facts), so a fact that stops matching leaves no stale keys.
func refreshSecurityAlertReconciliationKeys(
	ctx context.Context,
	database db.ExecQueryer,
	envelopes []facts.Envelope,
) error {
	if database == nil || len(envelopes) == 0 {
		return nil
	}

	factIDs := acceptedFactIDs(envelopes)
	if len(factIDs) == 0 {
		return nil
	}
	if _, err := database.ExecContext(ctx, deleteSecurityAlertReconciliationKeysSQL, array.StringArray(factIDs)); err != nil {
		return fmt.Errorf("delete security alert reconciliation keys: %w", err)
	}

	rows := securityAlertReconciliationKeyRows(envelopes)
	if len(rows) == 0 {
		return nil
	}
	for start := 0; start < len(rows); start += securityAlertReconciliationKeyBatchSize {
		end := start + securityAlertReconciliationKeyBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := insertSecurityAlertReconciliationKeyBatch(ctx, database, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func insertSecurityAlertReconciliationKeyBatch(
	ctx context.Context,
	database db.ExecQueryer,
	rows []securityAlertReconciliationKeyRow,
) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*columnsPerSecurityAlertReconciliationKeyRow)
	var values strings.Builder
	for i, row := range rows {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerSecurityAlertReconciliationKeyRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d)",
			offset+1,
			offset+2,
			offset+3,
			offset+4,
			offset+5,
		)
		args = append(args, row.FactID, row.ScopeID, row.GenerationID, row.KeyKind, row.KeyValue)
	}
	if _, err := database.ExecContext(ctx, insertSecurityAlertReconciliationKeyPrefix+values.String()+insertSecurityAlertReconciliationKeySuffix, args...); err != nil {
		return fmt.Errorf("insert security alert reconciliation key batch (%d rows): %w", len(rows), err)
	}
	return nil
}

// SecurityAlertReconciliationKeysReady reports whether the keyed
// security-alert reader may use the sidecar: the backfill marker is present.
// Both the writer (per-save refresh) and the backfill write rows, but only
// the marker proves every eligible fact has keys; until then the legacy
// query runs verbatim.
func SecurityAlertReconciliationKeysReady(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, securityAlertReconciliationKeysReadySQL, SecurityAlertReconciliationKeyBackfillMarker)
	if err != nil {
		return false, fmt.Errorf("check security alert reconciliation keys readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var ready bool
	if err := rows.Scan(&ready); err != nil {
		return false, fmt.Errorf("scan security alert reconciliation keys readiness: %w", err)
	}
	return ready, rows.Err()
}

const securityAlertReconciliationKeysReadySQL = `
SELECT EXISTS (
    SELECT 1 FROM security_alert_reconciliation_key_backfill_markers WHERE marker_name = $1
)
`

const deleteSecurityAlertReconciliationKeysSQL = `
DELETE FROM security_alert_reconciliation_keys
WHERE fact_id = ANY($1::text[])
`

const insertSecurityAlertReconciliationKeyPrefix = `
INSERT INTO security_alert_reconciliation_keys (
    fact_id, scope_id, generation_id, key_kind, key_value
) VALUES `

const insertSecurityAlertReconciliationKeySuffix = `
ON CONFLICT (fact_id, key_kind, key_value) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id
`

// listActiveSecurityAlertReconciliationFactsKeyedQuery is the keyed form of
// listActiveSecurityAlertReconciliationFactsQuery (issue #10875). It selects
// the same fact surface through the sidecar with one EXISTS semi-join per
// legacy arm instead of six payload predicates. EXISTS — never a bare JOIN:
// a fact carries several keys, so a join would duplicate its row and corrupt
// the ORDER BY fact_id ASC / LIMIT cursor paging the caller relies on. The
// kind surface, tombstone and generation-status gates, empty-filter early
// return, and ($5, $6) cursor paging are unchanged from the legacy query.
const listActiveSecurityAlertReconciliationFactsKeyedQuery = `
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
    'reducer_package_consumption_correlation',
    'reducer_supply_chain_impact_finding'
)
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      EXISTS (
          SELECT 1 FROM security_alert_reconciliation_keys AS keys
          WHERE keys.fact_id = fact.fact_id
            AND keys.scope_id = fact.scope_id
            AND keys.generation_id = fact.generation_id
            AND keys.key_kind = 'repository' AND keys.key_value = ANY($1::text[])
      )
      OR EXISTS (
          SELECT 1 FROM security_alert_reconciliation_keys AS keys
          WHERE keys.fact_id = fact.fact_id
            AND keys.scope_id = fact.scope_id
            AND keys.generation_id = fact.generation_id
            AND keys.key_kind = 'package' AND keys.key_value = ANY($2::text[])
      )
      OR EXISTS (
          SELECT 1 FROM security_alert_reconciliation_keys AS keys
          WHERE keys.fact_id = fact.fact_id
            AND keys.scope_id = fact.scope_id
            AND keys.generation_id = fact.generation_id
            AND keys.key_kind = 'cve' AND keys.key_value = ANY($3::text[])
      )
      OR EXISTS (
          SELECT 1 FROM security_alert_reconciliation_keys AS keys
          WHERE keys.fact_id = fact.fact_id
            AND keys.scope_id = fact.scope_id
            AND keys.generation_id = fact.generation_id
            AND keys.key_kind = 'advisory' AND keys.key_value = ANY($4::text[])
      )
  )
  AND ($5 = '' OR fact.fact_id > $5)
ORDER BY fact.fact_id ASC
LIMIT $6
`
