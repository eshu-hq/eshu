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

// The per-support table (migration 338, #10880 PR-B1) holds one row per live
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

// supplyChainImpactSupportColumn is one derived column of a support row: its
// name and the SQL expression over fact_records alias "fact" that fills it.
type supplyChainImpactSupportColumn struct {
	name, expr string
}

// supplyChainImpactSupportColumns lists every column except the primary key,
// in table order. The payload is the fact payload verbatim, byte for byte: PR-C
// makes the supports table the only copy, so a stripped key would be lost data.
// A replay that carries a new intent id therefore rewrites the support, as it
// rewrites the fact. The upsert's SET list and its write-on-change guard both
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
	{"payload", "fact.payload"},
}

// supplyChainImpactSupportFillOnlyColumns are columns the upsert fills and sets
// on a row it rewrites anyway but never compares (#10880 PR-C1 a). stable_fact_key
// links a finding across generations and is copied from the fact so the stored
// read can leave fact_records; it is outside the write-on-change guard, so a
// replay does not rewrite a row only to fill it, and a row written before the
// column existed keeps NULL until a real change rewrites it (the stored read
// falls back to the fact's own key for those).
var supplyChainImpactSupportFillOnlyColumns = []supplyChainImpactSupportColumn{
	{"stable_fact_key", "fact.stable_fact_key"},
}

// supplyChainImpactSupportsSelectSQL renders the SELECT that derives support
// rows from live finding facts, filtered by predicate, in fact id order.
func supplyChainImpactSupportsSelectSQL(predicate string) string {
	return supplyChainImpactSupportsSelectSQLWith(predicate, supplyChainImpactSupportFillOnlyColumns)
}

// supplyChainImpactSupportsSelectSQLWith is supplyChainImpactSupportsSelectSQL
// over an explicit fill-only column list. The differential test renders it with
// none to prove the shipped statements differ from the version-1 pin only by the
// fill-only columns.
func supplyChainImpactSupportsSelectSQLWith(predicate string, fillOnly []supplyChainImpactSupportColumn) string {
	var exprs []string
	for _, column := range supplyChainImpactSupportColumns {
		exprs = append(exprs, "    "+column.expr+" AS "+column.name)
	}
	for _, column := range fillOnly {
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

// supplyChainImpactSupportsColumnNamesWith returns fact_id, every derived column,
// then the fill-only columns (see supplyChainImpactSupportsSelectSQLWith).
func supplyChainImpactSupportsColumnNamesWith(fillOnly []supplyChainImpactSupportColumn) string {
	names := []string{"fact_id"}
	for _, column := range supplyChainImpactSupportColumns {
		names = append(names, column.name)
	}
	for _, column := range fillOnly {
		names = append(names, column.name)
	}
	return strings.Join(names, ", ")
}

// upsertSupplyChainImpactSupportsSQL writes one support row per live fact id
// ($1), in fact id order so two writers on overlapping fact ids lock rows in one
// order. The WHERE on DO UPDATE is the write-on-change guard: a row whose derived
// columns already match (the payload compared without the per-pass keys, see
// supplyChainImpactSupportsGuardIgnoredPayloadKeys) is not rewritten, so a replay
// produces no row version and no index change; the conflicting row is still locked
// before the guard runs, which costs one heap lock WAL record per row (measured in the
// B3 lab: 10,000 lock records of 54 bytes for 10,000 suppressed rows).
// RETURNING (xmax = 0) distinguishes an insert from an update.
var upsertSupplyChainImpactSupportsSQL = supplyChainImpactUpsertSupportsStatement(
	supplyChainImpactSupportsSelectSQL("fact.fact_id = ANY($1::text[])"),
)

// supplyChainImpactUpsertSupportsStatement wraps a support SELECT in the
// upsert shared by the writer and the backfill, so both write the same rows the
// same way.
func supplyChainImpactUpsertSupportsStatement(selectSQL string) string {
	return supplyChainImpactUpsertSupportsStatementWith(selectSQL, supplyChainImpactSupportFillOnlyColumns)
}

// supplyChainImpactSupportsGuardIgnoredPayloadKeys are the per-pass payload keys the
// write-on-change guard leaves out of its payload compare (#10880 B3a, arbiter ruling
// 2026-11-24). Every pass stamps the pass's own intent id and cause into the finding
// payload, so an identical replay differs from the stored row in exactly these two keys
// and, compared verbatim, rewrote every supports row (measured: 10,000 of 10,000). The
// stored payload stays verbatim; only the compare ignores them, on both sides, so a row
// that really changed (any other key, `evidence_fact_ids` and `generation_id` included)
// is still rewritten, and its stored intent_id and cause then name the pass that last
// changed it. `generation_id` is not listed: it cannot differ for one fact id.
var supplyChainImpactSupportsGuardIgnoredPayloadKeys = []string{"intent_id", "cause"}

// guardPayloadTerm is one side of the guard's payload compare with ignored keys
// removed (jsonb minus text); with no ignored keys it is the column itself.
func guardPayloadTerm(column string, ignored []string) string {
	if len(ignored) == 0 {
		return column
	}
	term := column
	for _, key := range ignored {
		term += " - '" + key + "'"
	}
	return "(" + term + ")"
}

// supplyChainImpactUpsertSupportsStatementWith is the upsert over an explicit
// fill-only column list (see supplyChainImpactSupportsSelectSQLWith), with the shipped
// guard that ignores the per-pass payload keys.
func supplyChainImpactUpsertSupportsStatementWith(selectSQL string, fillOnly []supplyChainImpactSupportColumn) string {
	return supplyChainImpactUpsertSupportsStatementGuarded(selectSQL, fillOnly, supplyChainImpactSupportsGuardIgnoredPayloadKeys)
}

// supplyChainImpactUpsertSupportsStatementGuarded is the upsert with an explicit list
// of payload keys the write-on-change compare ignores; nil renders the guard that
// compares the payload verbatim (the statement before B3a, kept for the differential
// and the mixed-version proof).
func supplyChainImpactUpsertSupportsStatementGuarded(selectSQL string, fillOnly []supplyChainImpactSupportColumn, ignored []string) string {
	var set, existing, incoming []string
	for _, column := range supplyChainImpactSupportColumns {
		set = append(set, column.name+" = EXCLUDED."+column.name)
		if column.name == "payload" {
			existing = append(existing, guardPayloadTerm("supports.payload", ignored))
			incoming = append(incoming, guardPayloadTerm("EXCLUDED.payload", ignored))
			continue
		}
		existing = append(existing, "supports."+column.name)
		incoming = append(incoming, "EXCLUDED."+column.name)
	}
	for _, column := range fillOnly {
		set = append(set, column.name+" = EXCLUDED."+column.name)
	}
	return `INSERT INTO supply_chain_impact_finding_supports AS supports (` + supplyChainImpactSupportsColumnNamesWith(fillOnly) + `)
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

var _ supplychaincore.SupplyChainImpactStoredKeyRecorder = (*supplyChainImpactTx)(nil)

// RecordStoredKeyMissing holds the count of stored findings the writer read with
// no stable key (see SupplyChainImpactStoredKeyRecorder) and counts it when the
// transaction commits, like the support rows: a rollback discards it. A
// transaction without instruments, or a zero count, holds nothing.
func (t *supplyChainImpactTx) RecordStoredKeyMissing(ctx context.Context, rows int64) {
	if t.instruments == nil || rows <= 0 {
		return
	}
	t.pendingStoredKeyMissing += rows
	t.captureCtx = context.WithoutCancel(ctx)
}

// flushStoredKeyMissing counts the held stored-key-missing rows and clears them.
func (t *supplyChainImpactTx) flushStoredKeyMissing() {
	if t.pendingStoredKeyMissing > 0 {
		t.instruments.RecordSupplyChainImpactStoredKeyMissing(t.captureCtx, t.pendingStoredKeyMissing)
	}
	t.pendingStoredKeyMissing = 0
}
