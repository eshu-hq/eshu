// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
)

// Defaults of the chunked consume (#9805). The per-tick bound is on
// post-expansion identifiers; the kept facts the 291 five-tuple probe builds
// per identifier decide the heap and apply work, so no candidate-row budget
// is needed. The identifier default is derived from the arbiter-required
// linearity probe (EXPLAIN ANALYZE BUFFERS of the apply statement at 400
// vs 4,000 identifiers, stored scale 5,000 winners, warm cache, disposable
// Postgres 18): 31.6ms vs 281.7ms, i.e. apply scales linearly at ~70us per
// identifier with a few ms of fixed winner-body cost (a third point at 1,000
// identifiers, 74.5ms, sits on the same line). The QA calibration uses
// the accepted QA anchors (shim 2026-11-10: ~71k stored winners, pre-chunk
// apply p95 4.04s at ~865 keys p95, i.e. a ~5ms/key upper envelope; ~13 kept
// facts per identifier): 200 identifiers imply on the order of a one-second
// apply, 5x under the 15s per-tick bar with headroom for cold I/O, and a
// ~10.5k-row QA backlog drains in short spaced ticks. The deleted node
// (identifier Semi Join over a stored-winners Seq Scan, 295 buffers at 5k
// stored) extrapolates to tens of milliseconds at the QA stored count, far
// below the narrow (B)-trigger's 1.5s, so no winners-table index is added.
//
//   - 5,000 admitted dirty rows bound the window the expansion reads; QA's
//     ~10.5k backlog at shim time is at least three windows. The existing
//     50,000-identifier cap still sends a backlog whose expansion exceeds it
//     to the full rebuild.
const (
	defaultWinnersChunkMaxDirtyRows = 5000
	defaultWinnersChunkMaxIdents    = 200
)

// winnersChunkBounds is one keyed transaction's resolved chunk bounds.
type winnersChunkBounds struct {
	dirtyRows   int64
	identifiers int64
}

func (s SupplyChainImpactWinnersStore) chunkBounds() winnersChunkBounds {
	b := winnersChunkBounds{
		dirtyRows:   int64(s.ChunkMaxDirtyRows),
		identifiers: int64(s.ChunkMaxIdentifiers),
	}
	if b.dirtyRows <= 0 {
		b.dirtyRows = defaultWinnersChunkMaxDirtyRows
	}
	if b.identifiers <= 0 {
		b.identifiers = defaultWinnersChunkMaxIdents
	}
	return b
}

// winnersChunkOutcome is what one bounded build admitted: the identifiers it
// applies and whether the identifier set continued past the chunk.
type winnersChunkOutcome struct {
	identifiers int64
	truncated   bool
}

// selectWinnersKeyedChunkIdentsSQL lists the identifier set's five-tuples
// oldest first (#10706), $1 of them: MIN(captured_at), tie-break MIN(seq),
// then the five-tuple for determinism. The caller asks for one past the
// bound, so a full page proves the set continues past this chunk. The first
// identifier always completes, so even a bound of one drains.
const selectWinnersKeyedChunkIdentsSQL = `
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM ` + winnersKeyedIdentsTable + `
ORDER BY age_captured_at, age_seq, package_id, repository_id, subject_digest, c0, advisory_id
LIMIT $1
`

// createWinnersKeyedChunkIdentsSQL and insertWinnersKeyedChunkIdentPrefix record
// the identifiers this chunk applies, for the identifier-table restriction
// and the applied-row delete below.
const createWinnersKeyedChunkIdentsSQL = `
CREATE TEMP TABLE winners_keyed_chunk_idents (
    package_id text, repository_id text, subject_digest text, c0 text, advisory_id text
) ON COMMIT DROP
`

// indexWinnersKeyedChunkIdentsSQL turns the chunk-identifier probes of the
// scope-remainder convert and the applied delete into index probes: without
// it each of the expansion's 42k-class rows checks chunk membership with a
// sequential scan, the statistics-blind nested loop the capacity lane (#10839)
// bounds. Unique because the chunk is filled from the distinct identifier
// table. Session-local to the tick transaction, like the table.
const indexWinnersKeyedChunkIdentsSQL = `
CREATE UNIQUE INDEX winners_keyed_chunk_idents_all_idx ON winners_keyed_chunk_idents (package_id, repository_id, subject_digest, c0, advisory_id)
`

// analyzeWinnersKeyedChunkIdentsSQL gives the planner the real chunk size
// before the convert and the applied delete plan on it.
const analyzeWinnersKeyedChunkIdentsSQL = `ANALYZE ` + winnersKeyedChunkIdentsTable

// restrictWinnersKeyedIdentsToChunkSQL drops the identifiers this chunk does
// not apply, so the probe and the apply recompute (and delete stored winners)
// only inside the chunk. The dropped identifiers are not lost: their dirty
// rows were never deleted, or were converted to fact rows below.
var restrictWinnersKeyedIdentsToChunkSQL = `
DELETE FROM ` + winnersKeyedIdentsTable + ` AS ident
WHERE NOT ` + winnersKeyedChunkIdentMatch("ident") + `
`

// winnersKeyedChunkIdentMatch reports, for a row carrying the five identifier
// columns under alias, whether the chunk applies its identifier.
func winnersKeyedChunkIdentMatch(alias string) string {
	return `EXISTS (
    SELECT 1 FROM ` + winnersKeyedChunkIdentsTable + ` AS chunk
    WHERE (chunk.package_id, chunk.repository_id, chunk.subject_digest, chunk.c0, chunk.advisory_id)
        = (` + alias + `.package_id, ` + alias + `.repository_id, ` + alias + `.subject_digest, ` + alias + `.c0, ` + alias + `.advisory_id)
)`
}

// buildWinnersKeyedChunk admits the chunk: the oldest identifiers of the
// post-expansion identifier set by producing dirty row (#10706
// oldest-first), one batched statement into the chunk table, then restricts
// the identifier table to them so the probe and the apply that follow are
// bounded by the same chunk. The first identifier always completes, so a
// single hot identifier still makes progress and the log always drains. It
// reports whether the set continued past the chunk.
func buildWinnersKeyedChunk(
	ctx context.Context,
	tx db.Transaction,
	bounds winnersChunkBounds,
) (winnersChunkOutcome, error) {
	var outcome winnersChunkOutcome
	rows, err := tx.QueryContext(ctx, selectWinnersKeyedChunkIdentsSQL, bounds.identifiers+1)
	if err != nil {
		return outcome, fmt.Errorf("list supply chain impact winners chunk identifiers: %w", err)
	}
	var idents []winnersKeyedChunkIdent
	for rows.Next() {
		var id winnersKeyedChunkIdent
		if err := rows.Scan(&id.pkg, &id.repo, &id.digest, &id.c0, &id.advisory); err != nil {
			_ = rows.Close()
			return outcome, fmt.Errorf("scan supply chain impact winners chunk identifier: %w", err)
		}
		idents = append(idents, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return outcome, fmt.Errorf("list supply chain impact winners chunk identifiers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return outcome, fmt.Errorf("close supply chain impact winners chunk identifiers: %w", err)
	}
	if int64(len(idents)) > bounds.identifiers {
		outcome.truncated = true
		idents = idents[:bounds.identifiers]
	}
	if _, err := tx.ExecContext(ctx, createWinnersKeyedChunkIdentsSQL); err != nil {
		return outcome, fmt.Errorf("create supply chain impact winners chunk identifiers: %w", err)
	}
	// One batched INSERT carries the whole chunk (#10839): the admitted set
	// is identical to the per-row loop it replaces (same rows, same
	// oldest-first order), but a saturated tick spends one round trip here
	// instead of one per identifier. The first identifier still always
	// completes, so a single hot identifier keeps draining.
	if len(idents) > 0 {
		if err := ctx.Err(); err != nil {
			return outcome, fmt.Errorf("build supply chain impact winners chunk: %w", err)
		}
		query, args, err := winnersKeyedChunkIdentBatch(idents)
		if err != nil {
			return outcome, fmt.Errorf("record supply chain impact winners chunk identifiers: %w", err)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return outcome, fmt.Errorf("record supply chain impact winners chunk identifiers: %w", err)
		}
	}
	outcome.identifiers = int64(len(idents))
	if _, err := tx.ExecContext(ctx, indexWinnersKeyedChunkIdentsSQL); err != nil {
		return outcome, fmt.Errorf("index supply chain impact winners chunk identifiers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedChunkIdentsSQL); err != nil {
		return outcome, fmt.Errorf("analyze supply chain impact winners chunk identifiers: %w", err)
	}
	if outcome.truncated {
		if _, err := tx.ExecContext(ctx, restrictWinnersKeyedIdentsToChunkSQL); err != nil {
			return outcome, fmt.Errorf("restrict supply chain impact winners identifiers to chunk: %w", err)
		}
		if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedIdentsSQL); err != nil {
			return outcome, fmt.Errorf("analyze supply chain impact winners chunk identifiers: %w", err)
		}
	}
	return outcome, nil
}

// The NVD baseline trip line's drain tally (eshu-hq/eshu#10767). The
// maintainer consumes the winners dirty log with DELETE, so the rows still in
// the log are only the ones it has not consumed yet. The trip sampler needs the
// hourly inflow instead, so the four drain statements
// (deleteWinnersKeyedAppliedDirtySQL and deleteWinnersKeyedAllDirtySQL, in
// supply_chain_impact_winners_keyed_settle_sql.go, plus the two full-resweep
// drains sharing winnersDirtyTallyCTEs) also add the NVD-proxy,
// GHSA-proxy (#11078), and NVD scope rows they delete to
// supply_chain_impact_winners_dirty_tally, keyed by capture minute,
// in the same statement and therefore the same transaction (migrations 299
// and 328). The capture statements are not touched.

// Winners dirty-log row predicates of the NVD baseline trip line, over the
// RETURNING columns of the DELETE. The ruled definition of #10728 decision 3
// counts fact rows whose advisory_id starts with CVE- (OSV carries CVE ids
// too, so the proxy over-counts and trips early, the safe direction). An NVD
// scope row is a baseline or window commit's pointer flip: it carries no
// advisory id by migration 272's shape constraint, so it is recognized by its
// scope id family. The trip sampler (vulnerabilitystore) states the same two
// predicates over the live log; the live proof drains and samples one
// database so any drift between them fails.
const (
	winnersNVDProxyRowPredicate = `kind = 'fact' AND advisory_id LIKE 'CVE-%'`
	winnersNVDScopeRowPredicate = `kind = 'scope' AND starts_with(scope_id, '` + vulnsource.NVDScopePrefix + `')`
	// winnersGHSAProxyRowPredicate is the GHSA leg of the NVD/GHSA inflow
	// split (#11078): fact rows whose advisory_id starts with GHSA-,
	// tallied into ghsa_proxy_rows by the same drain statements that keep
	// the NVD legs. It rides beside the ruled CVE-% rate and never folds
	// into the trip comparison.
	winnersGHSAProxyRowPredicate = `kind = 'fact' AND advisory_id LIKE 'GHSA-%'`
)

// winnersDirtyTallyCTEs are the two data-modifying CTEs appended to a drain
// statement whose `deleted` CTE returns source, dirty_version, kind,
// captured_at, advisory_id and scope_id. tallied adds the deleted NVD-proxy,
// GHSA-proxy (#11078), and NVD scope rows captured in the trailing two hours
// to their capture-minute bucket (older rows can never fall inside a trip
// sample's trailing hour, so a 58 hour old backlog chunk writes nothing).
// pruned removes buckets older than three hours; the two never touch the
// same row, and the drain runs under the single-flight resweep lock, so the
// tally has one writer at a time. Every data-modifying CTE runs to
// completion although the outer SELECT does not read it. All three tallies
// sum dirty_version (#10827 R2): one log row collapses every re-mark of its
// key, so the drain counts marks, not rows.
const winnersDirtyTallyCTEs = `,
tallied AS (
    INSERT INTO supply_chain_impact_winners_dirty_tally AS t (bucket, nvd_proxy_rows, nvd_scope_rows, ghsa_proxy_rows)
    SELECT date_trunc('minute', captured_at),
           COALESCE(sum(dirty_version) FILTER (WHERE ` + winnersNVDProxyRowPredicate + `), 0)::bigint,
           COALESCE(sum(dirty_version) FILTER (WHERE ` + winnersNVDScopeRowPredicate + `), 0)::bigint,
           COALESCE(sum(dirty_version) FILTER (WHERE ` + winnersGHSAProxyRowPredicate + `), 0)::bigint
    FROM deleted
    WHERE captured_at > clock_timestamp() - interval '2 hours'
      AND ((` + winnersNVDProxyRowPredicate + `) OR (` + winnersNVDScopeRowPredicate + `) OR (` + winnersGHSAProxyRowPredicate + `))
    GROUP BY 1
    ON CONFLICT (bucket) DO UPDATE
    SET nvd_proxy_rows = t.nvd_proxy_rows + EXCLUDED.nvd_proxy_rows,
        nvd_scope_rows = t.nvd_scope_rows + EXCLUDED.nvd_scope_rows,
        ghsa_proxy_rows = t.ghsa_proxy_rows + EXCLUDED.ghsa_proxy_rows
),
pruned AS (
    DELETE FROM supply_chain_impact_winners_dirty_tally
    WHERE bucket < clock_timestamp() - interval '3 hours'
)
`

// settleWinnersKeyedDirtyLog makes the dirty log match what the chunk
// applied, inside the tick transaction: it converts a partially applied
// scope row's remainder to fact rows, deletes exactly the applied rows
// (deleteWinnersKeyedAppliedDirtySQL, or the admitted rows for the
// full-rebuild fallback), tallies the deleted rows by capture source, counts
// what remains, and reads the remainder's earliest capture time. The order is
// a named constraint (#10706): convert runs before delete so a scope row's
// deletion and its remainder rows' insertion commit atomically, and the min
// is read after the delete so the horizon covers exactly the residual set.
// Converted remainder rows keep their scope row's captured_at (#11145), so the
// horizon stays under the scope mark until the remainder applies. Callers
// stamp S when nothing remains, else H = min minus 1us.
//
// R2 (#10827): both drains are single-statement lock-then-deletes. The locked
// CTE keeps only rows whose log version still equals the admitted version and
// locks them in the global key order (winnersDirtyKeyOrderClause) with
// FOR UPDATE, so concurrent re-marks stay queued. Ordering is per-statement:
// this tick converts, then deletes, and a writer pass captures, publishes,
// then retracts, so cross-batch inversion can still roll one victim back
// (#10851). The tally sums dirty_version: one deleted row counts every mark
// it collapsed. The returned bool reports that the remaining depth is an
// upper bound: the staging residual past its exact-count cap (#11129).
func settleWinnersKeyedDirtyLog(ctx context.Context, tx db.Transaction, fallback bool, converted *int64) (winnersCaptureTally, int64, sql.NullTime, bool, error) {
	deleteSQL := deleteWinnersKeyedAppliedDirtySQL
	if fallback {
		deleteSQL = deleteWinnersKeyedAllDirtySQL
	} else if n, err := convertWinnersKeyedScopeRemainder(ctx, tx); err != nil {
		return nil, 0, sql.NullTime{}, false, err
	} else if converted != nil {
		*converted = n
	}
	rows, err := tx.QueryContext(ctx, deleteSQL)
	if err != nil {
		return nil, 0, sql.NullTime{}, false, fmt.Errorf("delete applied supply chain impact winners dirty rows: %w", err)
	}
	tally := winnersCaptureTally{}
	for rows.Next() {
		var source string
		var n int64
		if err := rows.Scan(&source, &n); err != nil {
			_ = rows.Close()
			return nil, 0, sql.NullTime{}, false, fmt.Errorf("scan deleted supply chain impact winners dirty rows: %w", err)
		}
		tally[source] += n
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, sql.NullTime{}, false, fmt.Errorf("delete applied supply chain impact winners dirty rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, sql.NullTime{}, false, fmt.Errorf("close deleted supply chain impact winners dirty rows: %w", err)
	}
	remaining, err := scanWinnersInt64(ctx, tx, countWinnersKeyedRemainingSQL, "count remaining dirty rows")
	if err != nil {
		return nil, 0, sql.NullTime{}, false, err
	}
	remainingMin, err := selectWinnersRemainingMin(ctx, tx)
	if err != nil {
		return nil, 0, sql.NullTime{}, false, err
	}
	// The residual unions the staging queue (#10837 direction C): the
	// collapse folded at most its row budget before admission, so post-bound
	// and mid-tick staged rows stay queued and the horizon must cover them.
	staged, err := countWinnersStagingResidual(ctx, tx, "count remaining staging rows")
	if err != nil {
		return nil, 0, sql.NullTime{}, false, err
	}
	remaining, remainingMin = stagingUnionRemaining(remaining, remainingMin, staged)
	return tally, remaining, remainingMin, staged.upperBound, nil
}

// countWinnersKeyedKeptFacts counts the kept facts the identifier probe built.
// It runs before the operator facts are added, so it measures exactly what
// the identifier chunk implies per tick.
func countWinnersKeyedKeptFacts(ctx context.Context, tx db.Transaction) (int64, error) {
	return scanWinnersInt64(ctx, tx, countWinnersKeyedActiveFactsSQL, "count kept winner facts")
}

// horizonCandidate resolves the stamp candidate from the residual set: the
// horizon H = min(captured_at) minus 1us, so every unapplied row has
// captured_at above the stamped watermark. A non-empty remainder without a
// min is a bug, not an empty log: stamping S there would cover unapplied
// rows, so it fails the tick instead.
func horizonCandidate(remainingMin sql.NullTime) (time.Time, error) {
	if !remainingMin.Valid {
		return time.Time{}, fmt.Errorf("horizon min captured_at is NULL with a non-empty dirty log")
	}
	return remainingMin.Time.Add(-time.Microsecond), nil
}

// selectWinnersRemainingMin reads the dirty log's earliest capture time in
// the tick transaction. It reports none when the log is empty.
func selectWinnersRemainingMin(ctx context.Context, q db.Queryer) (sql.NullTime, error) {
	rows, err := q.QueryContext(ctx, selectWinnersKeyedRemainingMinSQL)
	if err != nil {
		return sql.NullTime{}, fmt.Errorf("read remaining min captured_at: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var min sql.NullTime
	if rows.Next() {
		if err := rows.Scan(&min); err != nil {
			return sql.NullTime{}, fmt.Errorf("read remaining min captured_at: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return sql.NullTime{}, fmt.Errorf("read remaining min captured_at: %w", err)
	}
	return min, nil
}

// winnersCaptureTally is the consumed dirty-log marks by capture source
// (#10827 R2): the drain sums dirty_version, so one consumed row counts every
// mark it collapsed, not one row.
type winnersCaptureTally map[string]int64

func (t winnersCaptureTally) total() int64 {
	var n int64
	for _, v := range t {
		n += v
	}
	return n
}

// triggerSources returns the consumed marks (version sums) of the sources a
// database trigger owns, the only ones the maintainer counts at consume.
func (t winnersCaptureTally) triggerSources() supplychaincore.WinnersTriggerCaptured {
	return supplychaincore.WinnersTriggerCaptured{
		Activation: t[telemetry.SupplyChainImpactWinnersSourceActivation],
		Operator:   t[telemetry.SupplyChainImpactWinnersSourceOperator],
		Scanner:    t[telemetry.SupplyChainImpactWinnersSourceScanner],
	}
}

// admitWinnersDirtyWindow copies the oldest limit dirty-log rows into the
// consumed temp table and returns how many it admitted. The rows stay in the
// log until settleWinnersKeyedDirtyLog deletes the applied ones. The copy
// carries each row's dirty_version for the drain's version recheck.
func admitWinnersDirtyWindow(ctx context.Context, tx db.Transaction, limit int64) (int64, error) {
	if _, err := tx.ExecContext(ctx, createWinnersKeyedConsumedSQL); err != nil {
		return 0, fmt.Errorf("admit supply chain impact winners dirty window: %w", err)
	}
	if _, err := tx.ExecContext(ctx, admitWinnersKeyedWindowSQL, limit); err != nil {
		return 0, fmt.Errorf("admit supply chain impact winners dirty window: %w", err)
	}
	if _, err := tx.ExecContext(ctx, indexWinnersKeyedConsumedSeqSQL); err != nil {
		return 0, fmt.Errorf("index supply chain impact winners consumed rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedConsumedSQL); err != nil {
		return 0, fmt.Errorf("analyze supply chain impact winners consumed rows: %w", err)
	}
	return scanWinnersInt64(ctx, tx, countWinnersKeyedConsumedSQL, "count admitted dirty rows")
}

// stampWinnersWatermark stamps the watermark candidate for a tick that
// consumed nothing and returns the watermark it replaced.
func stampWinnersWatermark(ctx context.Context, tx db.Transaction, candidate time.Time) (time.Time, error) {
	rows, err := tx.QueryContext(ctx, stampWinnersWatermarkSQL, candidate)
	if err != nil {
		return time.Time{}, fmt.Errorf("stamp supply chain impact winners watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var previous sql.NullTime
	if rows.Next() {
		if err := rows.Scan(&previous); err != nil {
			return time.Time{}, fmt.Errorf("scan supply chain impact winners watermark: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, fmt.Errorf("stamp supply chain impact winners watermark: %w", err)
	}
	if previous.Valid {
		return previous.Time.UTC(), nil
	}
	return time.Time{}, nil
}

// scanWinnersTime runs a one-row one-column timestamptz query, UTC-normalized,
// and drains its cursor. The tick clock and the committed watermark reads are
// both this shape.
func scanWinnersTime(ctx context.Context, q db.Queryer, query, what string) (time.Time, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, fmt.Errorf("%s: %w", what, err)
		}
		return time.Time{}, fmt.Errorf("%s: statement returned no row", what)
	}
	var at time.Time
	if err := rows.Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", what, err)
	}
	if rows.Next() {
		return time.Time{}, fmt.Errorf("%s: statement returned more than one row", what)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", what, err)
	}
	if err := rows.Close(); err != nil {
		return time.Time{}, fmt.Errorf("close %s: %w", what, err)
	}
	return at.UTC(), nil
}

// scanWinnersInt64 runs a one-row one-column int query and drains its cursor.
func scanWinnersInt64(ctx context.Context, q db.Queryer, query, what string) (int64, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	var n int64
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, fmt.Errorf("%s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	return n, nil
}
