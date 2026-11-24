// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// SQL of the staged winners audit sweep (#11131). The sweep replaces the
// whole-corpus staged audit tick with a walk over the keyed winners index
// (migration 291) in its key order: package_id, repository_id,
// subject_digest, c0, advisory_id. One range is one standby statement and one
// snapshot. It takes the next B index rows after the cursor, trims the last
// identifier when the budget cut through it, recomputes the winners of the
// identifiers it kept with the shipped winner body, and diffs them against the
// stored winners in the same key interval.
//
// The interval rules, each pinned by the T3 differential:
//   - A range is cut only at identifier edges, so a canonical key never spans
//     two ranges (every identifier field is a canonical-key component, and the
//     '|' guard below keeps the key-to-identifier map a function).
//   - An identifier with more than B facts runs alone and whole (oversize).
//   - The first range has no lower bound; the last range (fewer than B rows)
//     has no upper bound on either side, because stored winners whose facts
//     are gone can sort after every fact.
//   - Operator-suppression facts load whole on every range: they can lack the
//     identifier fields, and the shipped body joins their overrides by
//     canonical key, so only source keys of this range pick one up.

// winnersAuditRangeFactIdentExprs are the five keyed-index key expressions
// over alias f, byte-identical to migration 291 so the range predicate and the
// ORDER BY are index conditions and index order.
var winnersAuditRangeFactIdentExprs = []string{
	`(f.payload->>'package_id')`,
	`(f.payload->>'repository_id')`,
	`(f.payload->>'subject_digest')`,
	`COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '')`,
	`COALESCE(f.payload->>'advisory_id', '')`,
}

// winnersAuditRangeStoredIdentExprs are the same five identifiers over a
// stored winner row (alias w). The stored columns hold the coalesced payload
// values the winner body projected, so for a key whose facts carry no NULL
// identifier field (the NULL probe's guarantee) both sides compare equal.
var winnersAuditRangeStoredIdentExprs = []string{
	`w.package_id`,
	`w.repository_id`,
	`w.subject_digest`,
	`COALESCE(NULLIF(w.cve_id, ''), NULLIF(w.advisory_id, ''), '')`,
	`w.advisory_id`,
}

// winnersAuditRangeIdentColumns name the identifier columns the range CTEs
// carry, in key order.
var winnersAuditRangeIdentColumns = []string{"i_package_id", "i_repository_id", "i_subject_digest", "i_c0", "i_advisory_id"}

// winnersAuditRangeBoundColumns name the new-cursor columns of range_bounds.
var winnersAuditRangeBoundColumns = []string{"hi_package_id", "hi_repository_id", "hi_subject_digest", "hi_c0", "hi_advisory_id"}

func winnersAuditRangePrefixed(prefix string, columns []string) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = prefix + column
	}
	return out
}

func winnersAuditRangeRow(exprs []string) string {
	return "ROW(" + strings.Join(exprs, ", ") + ")"
}

func winnersAuditRangeOrder(exprs []string, direction string) string {
	out := make([]string, len(exprs))
	for i, expr := range exprs {
		out[i] = expr + direction
	}
	return strings.Join(out, ", ")
}

// winnersAuditRangeCursorParams are the cursor parameters $2..$6; $1 is the
// row budget in both variants.
var winnersAuditRangeCursorParams = []string{"$2::text", "$3::text", "$4::text", "$5::text", "$6::text"}

// winnersAuditRangeBoundSubselects read the new cursor from range_bounds as
// InitPlan parameters, so they can be index conditions.
func winnersAuditRangeBoundSubselects() []string {
	out := make([]string, len(winnersAuditRangeBoundColumns))
	for i, column := range winnersAuditRangeBoundColumns {
		out[i] = "(SELECT " + column + " FROM range_bounds)"
	}
	return out
}

// renderWinnersAuditRangeSQL renders the range statement. first renders the
// sweep's opening range, which has no lower bound and takes only $1.
//
// The fact CTEs project explicit columns, not fact.*, so every column the shared
// winner body reads off its fact alias (fact_id, scope_id, payload,
// source_confidence) must appear in range_raw, both range_facts arms and both
// active_facts arms; TestWinnersAuditRangeProjectsEveryColumnTheWinnerBodyReads
// derives that set from the body and fails on a gap (SQLSTATE 42703 otherwise).
func renderWinnersAuditRangeSQL(first bool) string {
	factRow := winnersAuditRangeRow(winnersAuditRangeFactIdentExprs)
	storedRow := winnersAuditRangeRow(winnersAuditRangeStoredIdentExprs)
	cursorRow := winnersAuditRangeRow(winnersAuditRangeCursorParams)
	factLower, storedLower := "", ""
	if !first {
		factLower = "\n      AND " + factRow + " > " + cursorRow
		storedLower = storedRow + " > " + cursorRow + "\n      AND "
	}
	rawIdent := winnersAuditRangeRow(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns))
	tailIdent := winnersAuditRangeRow(winnersAuditRangePrefixed("t.", winnersAuditRangeIdentColumns))
	boundRow := winnersAuditRangeRow(winnersAuditRangeBoundSubselects())

	identSelect := make([]string, len(winnersAuditRangeFactIdentExprs))
	for i, expr := range winnersAuditRangeFactIdentExprs {
		identSelect[i] = expr + " AS " + winnersAuditRangeIdentColumns[i]
	}
	oversizeMatch := make([]string, len(winnersAuditRangeFactIdentExprs))
	for i, expr := range winnersAuditRangeFactIdentExprs {
		oversizeMatch[i] = expr + " = " + winnersAuditRangeBoundSubselects()[i]
	}
	hiSelect := make([]string, len(winnersAuditRangeIdentColumns))
	for i, column := range winnersAuditRangeIdentColumns {
		hiSelect[i] = "CASE WHEN b.present THEN b." + column + " ELSE t." + column + " END AS " + winnersAuditRangeBoundColumns[i]
	}
	pipe := make([]string, len(winnersAuditRangeIdentColumns))
	for i, column := range winnersAuditRangeIdentColumns {
		pipe[i] = "strpos(" + column + ", '|') > 0"
	}
	identCols := strings.Join(winnersAuditRangeIdentColumns, ", ")

	return `WITH range_raw AS MATERIALIZED (
    SELECT f.fact_id, f.scope_id, f.generation_id, f.payload, f.source_confidence,
        ` + strings.Join(identSelect, ",\n        ") + `
    FROM fact_records AS f
    WHERE f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE` + factLower + `
    ORDER BY ` + strings.Join(winnersAuditRangeFactIdentExprs, ", ") + `
    LIMIT $1
),
range_tail AS MATERIALIZED (
    SELECT ` + identCols + `
    FROM range_raw
    ORDER BY ` + winnersAuditRangeOrder(winnersAuditRangeIdentColumns, " DESC") + `
    LIMIT 1
),
range_below_tail AS MATERIALIZED (
    SELECT TRUE AS present, ` + strings.Join(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns), ", ") + `
    FROM range_raw AS r, range_tail AS t
    WHERE ` + rawIdent + ` < ` + tailIdent + `
    ORDER BY ` + winnersAuditRangeOrder(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns), " DESC") + `
    LIMIT 1
),
range_bounds AS MATERIALIZED (
    SELECT c.n,
        c.n < $1 AS is_last,
        (c.n >= $1 AND b.present IS NULL) AS oversize,
        ` + strings.Join(hiSelect, ",\n        ") + `
    FROM (SELECT count(*) AS n FROM range_raw) AS c
    LEFT JOIN range_tail AS t ON TRUE
    LEFT JOIN range_below_tail AS b ON TRUE
),
range_facts AS MATERIALIZED (
    SELECT r.fact_id, r.scope_id, r.generation_id, r.payload, r.source_confidence, ` + strings.Join(winnersAuditRangePrefixed("r.", winnersAuditRangeIdentColumns), ", ") + `
    FROM range_raw AS r
    WHERE (SELECT NOT oversize FROM range_bounds)
      AND ((SELECT is_last FROM range_bounds) OR ` + rawIdent + ` <= ` + boundRow + `)
    UNION ALL
    SELECT f.fact_id, f.scope_id, f.generation_id, f.payload, f.source_confidence,
        ` + strings.Join(identSelect, ",\n        ") + `
    FROM fact_records AS f
    WHERE (SELECT oversize FROM range_bounds)
      AND f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE
      AND ` + strings.Join(oversizeMatch, "\n      AND ") + `
),
winners_now AS (
WITH active_facts AS NOT MATERIALIZED (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload, fact.source_confidence
    FROM range_facts AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE generation.status = 'active'
      AND fact.scope_id <> '` + supplyChainImpactOperatorScopeID + `'
    UNION ALL
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload, fact.source_confidence
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.scope_id = '` + supplyChainImpactOperatorScopeID + `'
      AND fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
)` + supplyChainImpactWinnerBodySQL + `
),
stored_range AS (
    SELECT w.*
    FROM supply_chain_impact_canonical_winners AS w
    WHERE ` + storedLower + `((SELECT is_last FROM range_bounds) OR ` + storedRow + ` <= ` + boundRow + `)
),
range_diff AS (
` + winnersAuditDiffSelect("winners_now", "stored_range") + `
)
SELECT 'b' AS row_kind, b.n::bigint AS n, b.is_last, b.oversize,
    ` + strings.Join(winnersAuditRangePrefixed("b.", winnersAuditRangeBoundColumns), ", ") + `,
    (SELECT count(*) FROM range_facts WHERE scope_id <> '` + supplyChainImpactOperatorScopeID + `')::bigint AS facts,
    COALESCE((SELECT bool_or(` + strings.Join(pipe, " OR ") + `) FROM range_facts
        WHERE scope_id <> '` + supplyChainImpactOperatorScopeID + `'), FALSE) AS pipe
FROM range_bounds AS b
UNION ALL
SELECT 'c', NULL::bigint, NULL::boolean, NULL::boolean,
    d.package_id, d.repository_id, d.subject_digest, d.c0, d.advisory_id,
    NULL::bigint, NULL::boolean
FROM range_diff AS d`
}

// selectWinnersAuditFirstRangeSQL opens a sweep: no lower bound, $1 = budget.
var selectWinnersAuditFirstRangeSQL = renderWinnersAuditRangeSQL(true)

// selectWinnersAuditNextRangeSQL continues a sweep after the cursor $2..$6,
// $1 = budget.
var selectWinnersAuditNextRangeSQL = renderWinnersAuditRangeSQL(false)

// probeWinnersAuditNullKeysSQL reports whether any live source finding fact
// has a NULL package_id, repository_id or subject_digest. The keyed index
// sorts such a row where no range predicate reaches it (a row comparison
// drops a row whose compared field is NULL), so a sweep counts as complete
// only when this probe reads all false at its start and at its end. Operator
// suppression facts are exempt: they may lack those fields by design and load
// whole on every range instead. Each EXISTS stops at its first match on the
// keyed index (a skip scan over the leading columns on PostgreSQL 18).
var probeWinnersAuditNullKeysSQL = `SELECT
    EXISTS (SELECT 1 FROM fact_records AS f
        WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.is_tombstone = FALSE
          AND (f.payload->>'package_id') IS NULL
          AND f.scope_id <> '` + supplyChainImpactOperatorScopeID + `'),
    EXISTS (SELECT 1 FROM fact_records AS f
        WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.is_tombstone = FALSE
          AND (f.payload->>'repository_id') IS NULL
          AND f.scope_id <> '` + supplyChainImpactOperatorScopeID + `'),
    EXISTS (SELECT 1 FROM fact_records AS f
        WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND f.is_tombstone = FALSE
          AND (f.payload->>'subject_digest') IS NULL
          AND f.scope_id <> '` + supplyChainImpactOperatorScopeID + `')`

// DefaultWinnersAuditRangeBudget is the default fact-row budget of one audit
// range (#11131). Measured on the QA replica, a 15,000-row range read cold in
// 20-21 s against the 25 s statement guard; 10,000 keeps about 14 s.
const DefaultWinnersAuditRangeBudget = 10000

// WinnersAuditRangeIdent is one identifier in keyed index order: the sweep
// cursor, or a range's new cursor. The fields compare in SQL only, never in
// Go, so the database collation decides the order on both sides.
type WinnersAuditRangeIdent struct {
	PackageID     string
	RepositoryID  string
	SubjectDigest string
	C0            string
	AdvisoryID    string
}

func (i WinnersAuditRangeIdent) args() []any {
	return []any{i.PackageID, i.RepositoryID, i.SubjectDigest, i.C0, i.AdvisoryID}
}

// WinnersAuditRange is what one range statement read: the bounds it covered,
// the facts it recomputed, the '|' guard, and the identifiers whose winners
// differ from the stored rows in the same key interval.
type WinnersAuditRange struct {
	// Lo is the cursor the range started after; nil opens the sweep.
	Lo *WinnersAuditRangeIdent
	// Hi is the range's last identifier, the next cursor. It is nil on the
	// last range, which is open-ended on the fact and the stored side.
	Hi *WinnersAuditRangeIdent
	// Last reports the range that reached the end of the index.
	Last bool
	// Oversize reports a range that is one identifier holding at least the
	// budget in facts; it ran alone and whole.
	Oversize bool
	// RawRows is the index rows the budgeted read took (at most the budget).
	RawRows int64
	// Facts is the fact rows the range recomputed.
	Facts int64
	// Pipe reports a range fact with '|' inside an identifier field. A '|'
	// can make two identifiers share a canonical key, so the range's diff
	// is not trusted and the sweep fails closed.
	Pipe bool
	// Budget is the row budget the read ran with.
	Budget int
	// Candidates are the differing identifiers.
	Candidates []WinnersAuditIdent
}

// ReadWinnersAuditRange runs one range statement on reader: the budget rows
// after lo (from the start when lo is nil), trimmed to whole identifiers. It
// takes no lock and writes nothing.
func ReadWinnersAuditRange(ctx context.Context, reader db.Queryer, lo *WinnersAuditRangeIdent, budget int) (WinnersAuditRange, error) {
	return readWinnersAuditRange(ctx, reader, lo, budget, selectWinnersAuditFirstRangeFromSupportsSQL, selectWinnersAuditNextRangeFromSupportsSQL)
}

// readWinnersAuditRange runs the range statement pair first/next; tests pass
// a seeded mutation of the shipped pair.
func readWinnersAuditRange(ctx context.Context, reader db.Queryer, lo *WinnersAuditRangeIdent, budget int, first, next string) (WinnersAuditRange, error) {
	var none WinnersAuditRange
	if reader == nil {
		return none, fmt.Errorf("winners audit range database is required")
	}
	if budget < 1 {
		return none, fmt.Errorf("winners audit range budget must be at least 1, got %d", budget)
	}
	query, args := first, []any{budget}
	if lo != nil {
		query = next
		args = append(args, lo.args()...)
	}
	rows, err := reader.QueryContext(ctx, query, args...)
	if err != nil {
		return none, fmt.Errorf("read winners audit range: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := WinnersAuditRange{Lo: lo, Budget: budget}
	sawBounds := false
	for rows.Next() {
		var (
			kind                            string
			n, facts                        sql.NullInt64
			isLast, oversize, pipe          sql.NullBool
			pkg, repo, digest, c0, advisory sql.NullString
		)
		if err := rows.Scan(&kind, &n, &isLast, &oversize, &pkg, &repo, &digest, &c0, &advisory, &facts, &pipe); err != nil {
			return none, fmt.Errorf("scan winners audit range: %w", err)
		}
		if kind == "b" {
			sawBounds = true
			out.RawRows, out.Facts = n.Int64, facts.Int64
			out.Last, out.Oversize, out.Pipe = isLast.Bool, oversize.Bool, pipe.Bool
			if !out.Last && pkg.Valid {
				out.Hi = &WinnersAuditRangeIdent{pkg.String, repo.String, digest.String, c0.String, advisory.String}
			}
			continue
		}
		out.Candidates = append(out.Candidates, WinnersAuditIdent{
			PackageID: pkg, RepositoryID: repo, SubjectDigest: digest, C0: c0, AdvisoryID: advisory,
		})
	}
	if err := rows.Err(); err != nil {
		return none, fmt.Errorf("read winners audit range: %w", err)
	}
	if !sawBounds {
		return none, fmt.Errorf("read winners audit range: statement returned no bounds row")
	}
	if !out.Last && out.Hi == nil {
		return none, fmt.Errorf("read winners audit range: a non-final range returned no upper bound")
	}
	return out, nil
}

// WinnersAuditNullKeys is the NULL probe's reading: which identifier fields
// some live finding fact leaves NULL.
type WinnersAuditNullKeys struct {
	PackageID     bool
	RepositoryID  bool
	SubjectDigest bool
}

// Any reports a NULL in any probed field.
func (k WinnersAuditNullKeys) Any() bool {
	return k.PackageID || k.RepositoryID || k.SubjectDigest
}

// ProbeWinnersAuditNullKeys runs the sweep's NULL probe on reader.
func ProbeWinnersAuditNullKeys(ctx context.Context, reader db.Queryer) (WinnersAuditNullKeys, error) {
	var none WinnersAuditNullKeys
	if reader == nil {
		return none, fmt.Errorf("winners audit null probe database is required")
	}
	rows, err := reader.QueryContext(ctx, probeWinnersAuditNullKeysFromSupportsSQL)
	if err != nil {
		return none, fmt.Errorf("probe winners audit null keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return none, fmt.Errorf("probe winners audit null keys: %w", err)
		}
		return none, fmt.Errorf("probe winners audit null keys: statement returned no row")
	}
	var out WinnersAuditNullKeys
	if err := rows.Scan(&out.PackageID, &out.RepositoryID, &out.SubjectDigest); err != nil {
		return none, fmt.Errorf("scan winners audit null keys: %w", err)
	}
	return out, rows.Err()
}
