// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
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
	return supplyChainImpactSupportsProjectionSQL(fillOnly) + `
FROM fact_records AS fact
WHERE fact.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND fact.is_tombstone = FALSE
  AND ` + predicate + `
ORDER BY fact.fact_id`
}

// supplyChainImpactSupportsProjectionSQL renders the SELECT list that derives a
// support row from a relation aliased "fact": the fact id, every derived column,
// then the fill-only columns. Every statement that derives support rows renders
// it, so the fact-derived reads and the in-memory upsert cannot disagree on a
// column expression.
func supplyChainImpactSupportsProjectionSQL(fillOnly []supplyChainImpactSupportColumn) string {
	var exprs []string
	for _, column := range supplyChainImpactSupportColumns {
		exprs = append(exprs, "    "+column.expr+" AS "+column.name)
	}
	for _, column := range fillOnly {
		exprs = append(exprs, "    "+column.expr+" AS "+column.name)
	}
	return `SELECT fact.fact_id,
` + strings.Join(exprs, ",\n")
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

// supplyChainImpactSupportRowsSelectSQL derives support rows from the writer's
// in-memory rows instead of from fact_records (#10880 PR-C2 h): the same
// projection over the same alias, with the relation an unnest of the six values
// a fact row contributes. The payload crosses as text and is cast to jsonb here,
// as the versioned fact insert casts it, so the stored bytes match what the
// fact-derived statement reads back from a fact row.
func supplyChainImpactSupportRowsSelectSQL() string {
	return supplyChainImpactSupportsProjectionSQL(supplyChainImpactSupportFillOnlyColumns) + `
FROM (
    SELECT t.fact_id, t.scope_id, t.generation_id, t.source_confidence, t.stable_fact_key,
           t.payload::jsonb AS payload
    FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
        AS t(fact_id, scope_id, generation_id, source_confidence, stable_fact_key, payload)
) AS fact
ORDER BY fact.fact_id`
}

// upsertSupplyChainImpactSupportRowsSQL is the writer's supports upsert: the
// shared upsert statement over the in-memory select, so the SET list and the
// write-on-change guard are the same ones the backfill's fact-derived upsert
// carries. Arguments: $1 fact ids, $2 scope ids, $3 generation ids, $4 source
// confidences, $5 stable fact keys, $6 payloads (JSON text), each one array
// element per row.
var upsertSupplyChainImpactSupportRowsSQL = supplyChainImpactUpsertSupportsStatement(
	supplyChainImpactSupportRowsSelectSQL(),
)

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

// UpsertImpactSupports writes the support rows of the live facts in factIDs,
// derived from fact_records. The writer no longer calls it (it writes the rows it
// holds through UpsertImpactSupportRows); the backfill, which reads the frozen
// facts, still does.
func (t *supplyChainImpactTx) UpsertImpactSupports(
	ctx context.Context,
	factIDs []string,
) (supplychaincore.SupplyChainImpactSupportsWrite, error) {
	var result supplychaincore.SupplyChainImpactSupportsWrite
	if len(factIDs) == 0 {
		return result, nil
	}
	inserted, updated, err := t.runSupportsUpsert(ctx, upsertSupplyChainImpactSupportsSQL, array.Of(factIDs))
	if err != nil {
		return result, err
	}
	result.Inserted, result.Updated = inserted, updated
	result.Unchanged = int64(len(factIDs)) - result.Inserted - result.Updated
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionInserted, result.Inserted)
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionUpdated, result.Updated)
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionUnchanged, result.Unchanged)
	return result, nil
}

// UpsertImpactSupportRows writes the support rows of the writer's in-memory
// finding rows inside the writer transaction (#10880 PR-C2 h): rows sharing a
// fact id collapse to the last one (as the fact insert collapsed them), and the
// rest go in fact id order in chunks of factwrite.BatchSize, each chunk one
// statement. A replay whose derived columns match writes no row version.
func (t *supplyChainImpactTx) UpsertImpactSupportRows(
	ctx context.Context,
	rows []factwrite.VersionedRow,
) (supplychaincore.SupplyChainImpactSupportsWrite, error) {
	var result supplychaincore.SupplyChainImpactSupportsWrite
	rows = factwrite.DedupeRowsByFactID(rows, func(r factwrite.VersionedRow) string { return r.FactID })
	// A support mirrors a live finding fact: the fact-derived statement reads only live rows of
	// the finding kind, so a tombstoned row or a row of another kind gets no support here either.
	ordered := make([]factwrite.VersionedRow, 0, len(rows))
	for _, row := range rows {
		if row.IsTombstone || row.FactKind != facts.ReducerSupplyChainImpactFindingFactKind {
			continue
		}
		ordered = append(ordered, row)
	}
	if len(ordered) == 0 {
		return result, nil
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].FactID < ordered[j].FactID })
	for start := 0; start < len(ordered); start += factwrite.BatchSize {
		end := min(start+factwrite.BatchSize, len(ordered))
		chunk := ordered[start:end]
		n := len(chunk)
		factIDs, scopeIDs, generationIDs := make([]string, n), make([]string, n), make([]string, n)
		confidences, stableKeys, payloads := make([]string, n), make([]string, n), make([]string, n)
		for i, row := range chunk {
			factIDs[i], scopeIDs[i], generationIDs[i] = row.FactID, row.ScopeID, row.GenerationID
			confidences[i], stableKeys[i], payloads[i] = row.SourceConfidence, row.StableFactKey, row.Payload
		}
		inserted, updated, err := t.runSupportsUpsert(ctx, upsertSupplyChainImpactSupportRowsSQL,
			array.Of(factIDs), array.Of(scopeIDs), array.Of(generationIDs),
			array.Of(confidences), array.Of(stableKeys), array.Of(payloads))
		if err != nil {
			return result, err
		}
		result.Inserted += inserted
		result.Updated += updated
	}
	result.Unchanged = int64(len(ordered)) - result.Inserted - result.Updated
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionInserted, result.Inserted)
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionUpdated, result.Updated)
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionUnchanged, result.Unchanged)
	return result, nil
}

// runSupportsUpsert runs one support upsert statement and counts its inserted
// and updated rows from the RETURNING (xmax = 0) flag.
func (t *supplyChainImpactTx) runSupportsUpsert(ctx context.Context, query string, args ...any) (inserted, updated int64, err error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, fmt.Errorf("upsert supply chain impact supports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var wasInserted bool
		if err := rows.Scan(&wasInserted); err != nil {
			return 0, 0, fmt.Errorf("scan supply chain impact support upsert: %w", err)
		}
		if wasInserted {
			inserted++
		} else {
			updated++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("upsert supply chain impact supports: %w", err)
	}
	return inserted, updated, nil
}

// RecordRetractedImpactSupports holds the supports the writer's retraction
// statement deleted (#10880 PR-C2 i: the statement deletes them itself, so this
// only counts them) and counts them with the pass's other support rows when the
// transaction commits.
func (t *supplyChainImpactTx) RecordRetractedImpactSupports(ctx context.Context, rows int64) {
	t.holdSupportsRows(ctx, telemetry.SupplyChainImpactSupportsActionDeleted, rows)
}

// DeleteRetractedImpactSupports removes the supports of the (scope, generation)
// whose fact the retraction tombstoned. The writer no longer calls it (its
// retraction statement deletes supports directly); the backfill, which reads
// frozen facts, still does.
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
