// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The per-support table (migration 336, #10880 PR-B1) holds one row per live
// finding fact. These statements derive every column from fact_records, never
// from in-memory findings, so the fact payload stays the one source of truth for
// the canonical key and the filter columns. The canonical key, finding id,
// priority, tiebreak flag, and suppression state come from the same helpers the
// winners select uses, and the severity bucket from the shared cvss.BucketSQL,
// so a support cannot drift from a winner built from the same fact.
//
// The table is a mirror of every live finding fact, the operator scope and the
// derived and scanner-associated rows included. The static-authority guard (no
// derived-image scope, no scanner association, static and active class only)
// stays on the read side, because the scanner classification rows are written by
// a separate flow after the finding fact: a write-time exclusion would race it.

// supplyChainImpactSupportPayloadExpr is the stored payload: the fact payload
// without the per-pass fields (intent_id, cause, generation_id, and the content
// digest) the winners row also drops. A replay of an unchanged finding carries a
// new intent id, and keeping it would rewrite every support on every pass; no
// reader serves those fields (the winners reads return the same stripped payload
// and match the legacy reads), so the write-on-change guard can hold.
const supplyChainImpactSupportPayloadExpr = `((fact.payload - 'intent_id' - 'cause' - 'generation_id') #- '{provenance,content_digest}')::jsonb`

// supplyChainImpactSupportColumn is one derived column of a support row: its
// name and the SQL expression over fact_records alias "fact" that fills it.
type supplyChainImpactSupportColumn struct {
	name, expr string
}

// supplyChainImpactSupportColumns lists every column except the primary key,
// in table order. The upsert's SET list and its write-on-change guard both
// derive from it, so a column cannot be updated without being compared.
var supplyChainImpactSupportColumns = []supplyChainImpactSupportColumn{
	{"canonical_key", supplyChainImpactCanonicalKeyConcat("fact.payload", "      ", "    ")},
	{"scope_id", "fact.scope_id"},
	{"generation_id", "fact.generation_id"},
	{"finding_id", supplyChainImpactFindingIDCoalesce("fact.payload")},
	{"has_payload_finding_id", supplyChainImpactHasPayloadFindingIDExpr("fact.payload")},
	{"priority_score", supplyChainImpactPriorityScoreExpr("fact.payload")},
	{"source_confidence", "fact.source_confidence"},
	{"suppression_state", supplyChainImpactSuppressionStateExpr("fact.payload")},
	{"impact_status", `COALESCE(fact.payload->>'impact_status', '')`},
	{"ecosystem", `COALESCE(LOWER((fact.payload->>'ecosystem') COLLATE "C") COLLATE "default", '')`},
	{"severity_bucket", cvss.BucketSQL("fact.payload")},
	{"repository_id", `COALESCE(fact.payload->>'repository_id', '')`},
	{"cve_id", `COALESCE(fact.payload->>'cve_id', '')`},
	{"advisory_id", `COALESCE(fact.payload->>'advisory_id', '')`},
	{"package_id", `COALESCE(fact.payload->>'package_id', '')`},
	{"subject_digest", `COALESCE(fact.payload->>'subject_digest', '')`},
	{"image_ref", `COALESCE(fact.payload->>'image_ref', '')`},
	{"priority_bucket", `COALESCE(fact.payload->>'priority_bucket', '')`},
	{"detection_profile", `COALESCE(fact.payload->>'detection_profile', '')`},
	{"observed_version", `COALESCE(fact.payload->>'observed_version', '')`},
	{"match_reason", `COALESCE(fact.payload->>'match_reason', '')`},
	{"payload", supplyChainImpactSupportPayloadExpr},
}

// supplyChainImpactSupportsSelectSQL renders the SELECT that derives support
// rows from live finding facts, filtered by predicate, in fact id order.
func supplyChainImpactSupportsSelectSQL(predicate string) string {
	var exprs []string
	for _, column := range supplyChainImpactSupportColumns {
		exprs = append(exprs, "    "+column.expr+" AS "+column.name)
	}
	return `SELECT fact.fact_id,
` + strings.Join(exprs, ",\n") + `
FROM fact_records AS fact
WHERE fact.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND fact.is_tombstone = FALSE
  AND ` + predicate + `
ORDER BY fact.fact_id`
}

// supplyChainImpactSupportsColumnNames returns fact_id then every derived column.
func supplyChainImpactSupportsColumnNames() string {
	names := []string{"fact_id"}
	for _, column := range supplyChainImpactSupportColumns {
		names = append(names, column.name)
	}
	return strings.Join(names, ", ")
}

// upsertSupplyChainImpactSupportsSQL writes one support row per live fact id
// ($1), in fact id order so two writers on overlapping fact ids lock rows in one
// order. The WHERE on DO UPDATE is the write-on-change guard: a row whose derived
// columns already match is not rewritten, so a replay produces no row version and
// no WAL. RETURNING (xmax = 0) distinguishes an insert from an update.
var upsertSupplyChainImpactSupportsSQL = supplyChainImpactUpsertSupportsStatement(
	supplyChainImpactSupportsSelectSQL("fact.fact_id = ANY($1::text[])"),
)

// supplyChainImpactUpsertSupportsStatement wraps a support SELECT in the
// upsert shared by the writer and the backfill, so both write the same rows the
// same way.
func supplyChainImpactUpsertSupportsStatement(selectSQL string) string {
	var set, existing, incoming []string
	for _, column := range supplyChainImpactSupportColumns {
		set = append(set, column.name+" = EXCLUDED."+column.name)
		existing = append(existing, "supports."+column.name)
		incoming = append(incoming, "EXCLUDED."+column.name)
	}
	return `INSERT INTO supply_chain_impact_finding_supports AS supports (` + supplyChainImpactSupportsColumnNames() + `)
` + selectSQL + `
ON CONFLICT (fact_id) DO UPDATE SET
    ` + strings.Join(set, ",\n    ") + `
WHERE (` + strings.Join(existing, ", ") + `)
      IS DISTINCT FROM (` + strings.Join(incoming, ", ") + `)
RETURNING (xmax = 0) AS inserted`
}

// deleteRetractedSupplyChainImpactSupportsSQL removes the supports of one
// (scope, generation) whose fact a retraction tombstoned. It reads the fact's
// state, not the retraction's keep set, so it follows exactly what the fenced
// retraction did: a row a fresher writer's token kept is not tombstoned, so its
// support stays.
const deleteRetractedSupplyChainImpactSupportsSQL = `DELETE FROM supply_chain_impact_finding_supports AS supports
USING fact_records AS fact
WHERE supports.scope_id = $1
  AND supports.generation_id = $2
  AND fact.fact_id = supports.fact_id
  AND fact.is_tombstone = TRUE`

var _ supplychaincore.SupplyChainImpactSupportsMaintainer = (*supplyChainImpactTx)(nil)

// UpsertImpactSupports writes the support rows of the live facts in factIDs
// inside the writer transaction (see SupplyChainImpactSupportsMaintainer).
func (t *supplyChainImpactTx) UpsertImpactSupports(
	ctx context.Context,
	factIDs []string,
) (supplychaincore.SupplyChainImpactSupportsWrite, error) {
	var result supplychaincore.SupplyChainImpactSupportsWrite
	if len(factIDs) == 0 {
		return result, nil
	}
	rows, err := t.tx.QueryContext(ctx, upsertSupplyChainImpactSupportsSQL, array.Of(factIDs))
	if err != nil {
		return result, fmt.Errorf("upsert supply chain impact supports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var inserted bool
		if err := rows.Scan(&inserted); err != nil {
			return result, fmt.Errorf("scan supply chain impact support upsert: %w", err)
		}
		if inserted {
			result.Inserted++
		} else {
			result.Updated++
		}
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("upsert supply chain impact supports: %w", err)
	}
	result.Unchanged = int64(len(factIDs)) - result.Inserted - result.Updated
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionInserted, result.Inserted)
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionUpdated, result.Updated)
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionUnchanged, result.Unchanged)
	return result, nil
}

// DeleteRetractedImpactSupports removes the supports of the (scope, generation)
// whose fact the retraction tombstoned.
func (t *supplyChainImpactTx) DeleteRetractedImpactSupports(
	ctx context.Context,
	scopeID, generationID string,
) (int64, error) {
	result, err := t.tx.ExecContext(ctx, deleteRetractedSupplyChainImpactSupportsSQL, scopeID, generationID)
	if err != nil {
		return 0, fmt.Errorf("delete retracted supply chain impact supports: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count retracted supply chain impact supports: %w", err)
	}
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionDeleted, removed)
	return removed, nil
}

// holdSupportsRows holds the rows a statement of this transaction wrote under
// action and counts them when the transaction commits; a rollback, or a commit
// that fails, discards them, so the counter matches the rows that exist. A zero
// count, or a transaction without instruments, holds nothing. The transaction is
// used by one writer goroutine, so the hold needs no lock.
func (t *supplyChainImpactTx) holdSupportsRows(ctx context.Context, action string, rows int64) {
	if t.instruments == nil || rows <= 0 {
		return
	}
	if t.pendingSupports == nil {
		t.pendingSupports = make(map[string]int64, 4)
	}
	t.pendingSupports[action] += rows
	t.captureCtx = context.WithoutCancel(ctx)
}

// flushSupports counts the held support rows and clears them.
func (t *supplyChainImpactTx) flushSupports() {
	for action, rows := range t.pendingSupports {
		t.instruments.RecordSupplyChainImpactSupportsRows(t.captureCtx, action, telemetry.SupplyChainImpactSupportsSourceWriter, rows)
	}
	t.pendingSupports = nil
}
