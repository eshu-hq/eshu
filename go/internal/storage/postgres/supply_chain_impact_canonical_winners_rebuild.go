// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres //nolint:filelength // 503 lines: the #11515 pre-stamp watermark read sits immediately before the rebuild statement it precedes; splitting the resweep flow would separate the stamp from its statement.

import (
	"context"
	"fmt"
	"strings"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// supplyChainImpactWinnerDataColumns lists every content column of
// supply_chain_impact_canonical_winners, in table order, without the key
// (canonical_key) and without materialized_at. The insert list, the change test
// and the conflict update are all derived from this one slice, so a column added
// to the winner select cannot be written but left out of the comparison: that
// would make an unchanged-looking row skip a real change.
var supplyChainImpactWinnerDataColumns = []string{
	"winner_fact_id", "winner_scope_id", "finding_id", "priority_score",
	"source_count", "impact_status", "ecosystem", "severity_bucket", "repository_id",
	"cve_id", "advisory_id", "package_id", "subject_digest", "image_ref", "priority_bucket",
	"detection_profile", "observed_version", "match_reason", "suppression_state", "suppression_expires_at",
	"service_ids", "workload_ids", "environments", "payload", "source_confidence",
}

// supplyChainImpactWinnerRowExpr renders ROW(prefix.col, ...) over the data
// columns. A row comparison with IS DISTINCT FROM treats NULL as a value, so a
// NULL suppression_expires_at on both sides is equal and NULL against a time is
// a change.
func supplyChainImpactWinnerRowExpr(prefix string) string {
	cols := make([]string, len(supplyChainImpactWinnerDataColumns))
	for i, c := range supplyChainImpactWinnerDataColumns {
		cols[i] = prefix + "." + c
	}
	return "ROW(" + strings.Join(cols, ", ") + ")"
}

func supplyChainImpactWinnerConflictSet() string {
	sets := make([]string, 0, len(supplyChainImpactWinnerDataColumns)+1)
	for _, c := range supplyChainImpactWinnerDataColumns {
		sets = append(sets, c+" = EXCLUDED."+c)
	}
	sets = append(sets, "materialized_at = EXCLUDED.materialized_at")
	return strings.Join(sets, ",\n        ")
}

// runSupplyChainImpactWinnersRebuild reconciles the winners table to the
// current active set and stamps the maintainer watermark, chunk by chunk
// (ruled #9805 v3-full), so a concurrent reader never sees a torn rebuild:
// every chunk commits inside this same transaction, and the watermark stamps
// once, after the last chunk.
//
// Each chunk recomputes its winners (winners_now) but writes only the rows
// that need it (#9774). The changed CTE keeps a winner that has no stored row
// or whose data columns differ from the stored row, and the INSERT reads from
// changed, so an unchanged row never reaches the INSERT and is never locked,
// versioned or written to WAL. A plain ON CONFLICT DO UPDATE ... WHERE would
// not do: it still locks every conflicting row, which writes WAL per row.
//
// The change test reads the stored table, so it is only correct against the
// last committed resweep. ResweepWinners runs the chunks after winning
// tryClaimSupplyChainImpactWinnersResweepSQL, in later statements of the same
// transaction, so their snapshots always see that commit. ON CONFLICT DO UPDATE
// is not a backstop for a resweep that skips the claim: it only covers rows
// the chunk chose to write, never a row it judged unchanged against a stale
// read. A mark that commits mid-walk lands in the residual the caller counts
// after the chunks and caps the stamp below its captured_at, so the next tick
// applies it; the walk itself never re-reads the drain.
//
// materialized_at is left out of the comparison and is set only when a row is
// written, so on a winner row it means "last content change". Readers do not
// use it: freshness comes from the singleton watermark below, which every
// resweep stamps whether or not a row changed.
//
// Each chunk deletes the stored winners of its identifier set that the chunk
// no longer produces; the sets tile the enumeration, so the union is the
// full delete. The enumeration unions the live candidate set with the stored
// winners' identifiers, so a stored-only key (tombstoned facts, a rotated
// canonical key, a row left by a resweep to zero) still lands in some chunk
// and its delete fires. The final watermark stamp (stampWinnersFullWatermarkSQL) is
// unconditional, so it stamps even a resweep whose chunks produced zero
// winners, which lets the read tell "never populated" (no watermark row) from
// "reswept to zero findings" (watermark present, table empty).
//
// stamp is the stamp ceiling S and the materialized_at of every written row.
// The watermark is GREATEST(previous, LEAST(S, residual min - 1us)) (#11117).
// Run it only through ResweepWinners or the keyed fallback.

// tryClaimSupplyChainImpactWinnersResweepSQL makes resweeps single-flight across
// every reducer instance, because the partition lease alone does not: its TTL
// can lapse during a long resweep and let a second instance start. It must run
// as its own statement before the rebuild. Under READ COMMITTED each statement
// takes a new snapshot, so the rebuild's snapshot is taken with the lock held
// and sees any resweep that committed first; the same call inside the rebuild
// statement would run after that statement's snapshot was already taken.
//
// It is a try, not a wait, so instances do not stack up full resweeps behind one
// another: a second resweep over the same facts adds nothing the next tick does
// not. The lock ends with the transaction, so a dead backend frees it at once,
// with no TTL to wait out. The class keys differ from the partition lease's
// (hashtext('shared_projection_partition_leases'), hashtext(domain)).
const tryClaimSupplyChainImpactWinnersResweepSQL = `
SELECT pg_try_advisory_xact_lock(hashtext('supply_chain_impact_winners'), hashtext('resweep'))
`

// pinWinnersFullReadCommittedSQL pins the full resweep's transaction to READ
// COMMITTED (#11117). It must be the transaction's first statement.
const pinWinnersFullReadCommittedSQL = `SET TRANSACTION ISOLATION LEVEL READ COMMITTED`

// selectSupplyChainImpactWinnersWatermarkSQL reads the singleton watermark row.
const selectSupplyChainImpactWinnersWatermarkSQL = `
SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1
`

// SupplyChainImpactWinnersStore maintains the supply_chain_impact_canonical_winners
// read model from the active impact facts.
type SupplyChainImpactWinnersStore struct {
	DB db.Queryer
	// staleCandidates is the optional stale-reader source for the winners-audit
	// candidate scan (#10416 slice 1). When set, the audit reads its differing
	// keys from this handle instead of building a temp table on the writer, and
	// seeds them into the writer transaction in bounded batches. It is set only
	// through WithStaleCandidates, only on the Auditor instance: the Rebuilder,
	// KeyedRebuilder, and DirtyLogReader instances of this same struct must
	// never carry one, since their reads feed locks, claims, and gauges in the
	// same cycle. Nil keeps the legacy writer-backed audit byte-identical.
	staleCandidates db.Queryer
	// MaxIdentifiers and MaxKeyFraction cap a keyed resweep (#9805): a tick whose
	// consumed dirty-log rows expand to more identifiers than MaxIdentifiers, or
	// to more than MaxKeyFraction of the stored winners, runs the full rebuild in
	// the same transaction. Zero means the defaults, 50,000 and 0.5.
	MaxIdentifiers int
	MaxKeyFraction float64
	// ChunkMaxDirtyRows and ChunkMaxIdentifiers bound one keyed transaction
	// (#9805 chunked consume): the dirty-log rows admitted as candidates and
	// the post-expansion identifiers probed before the tick commits its chunk
	// and leaves the rest queued. Zero means the defaults in
	// supply_chain_impact_winners_keyed_chunk.go.
	ChunkMaxDirtyRows   int
	ChunkMaxIdentifiers int
	// ChunkMaxScannedCopies bounds one keyed transaction by the pre-filter
	// copies the probe scans (Dec 6(ii)): the tick admits the oldest-first
	// budgeted prefix of the identifier set and leaves the rest queued. The
	// first identifier is always admitted and never split, and completes
	// iff its probe finishes within ResweepStatementTimeout below
	// (eshu-hq/eshu#11442). Zero means the default in
	// supply_chain_impact_winners_keyed_chunk.go.
	ChunkMaxScannedCopies int
	// ResweepStatementTimeout bounds every statement of a keyed tick
	// transaction (Dec 7 guard): a statement past the timeout aborts with
	// the distinct statement_timeout failure class instead of holding the
	// single-flight lock. The over-cap full fallback lifts it. Zero or
	// negative means the default in
	// supply_chain_impact_winners_keyed.go.
	ResweepStatementTimeout time.Duration
	// StagingCollapseMaxRows bounds one tick's staging collapse (#10837
	// direction C): at most this many staged rows fold into the dirty log
	// before admission. Zero means the default in
	// supply_chain_impact_winners_staging_collapse.go.
	StagingCollapseMaxRows int
	// Instruments records the wall time of each phase of a keyed tick (#9975).
	// Nil records nothing and changes no statement.
	Instruments *telemetry.Instruments
}

// NewSupplyChainImpactWinnersStore wraps a DB handle for winner maintenance.
func NewSupplyChainImpactWinnersStore(database db.Queryer) SupplyChainImpactWinnersStore {
	return SupplyChainImpactWinnersStore{DB: database}
}

// WithInstruments returns the store with the keyed tick's phase timers wired to
// in (#9975). It leaves the receiver unchanged.
func (s SupplyChainImpactWinnersStore) WithInstruments(in *telemetry.Instruments) SupplyChainImpactWinnersStore {
	s.Instruments = in
	return s
}

// ResweepWinners reconciles the entire winners table to the current active
// impact-fact set in one transaction and reports what it wrote. It writes a
// row version only for a winner that is new or changed, deletes winners no
// longer in the active set, and always stamps the watermark. It is the #3389
// backfill and the safe full-resweep backstop.
//
// Under the resweep lock it runs, in this order (#11117): read S from the DB
// clock, delete every dirty-log and staged mark visible to that delete, then
// run the rebuild. Marks are staged in the transaction that makes the change,
// so every mark the delete sees names a change the later rebuild snapshot
// sees too: the full recomputes every key, so it consumes what it covers.
// Deleting after the rebuild would delete marks of changes committed between
// the two snapshots and lose them. The rebuild stamps GREATEST(previous,
// LEAST(S, residual min - 1us)), so a mark committed after the drain stays
// queued with the watermark under it. A mark that executes before S and
// commits after the rebuild snapshot stays queued for the next tick, which
// applies it. A failed full rolls the deletes back with the rebuild.
//
// When another resweep is in flight it writes nothing, stamps nothing and
// returns Skipped. Any other non-nil result means the transaction committed; on
// an error the result is the zero value and nothing was written.
//
// The full records this binary's winners contract in the same transaction
// (#11117); ResweepWinnersUnlessCurrent and ResweepWinnersUnlessNewer are the
// guarded variants the maintainer runs.
func (s SupplyChainImpactWinnersStore) ResweepWinners(
	ctx context.Context,
) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	return s.resweepFull(ctx, winnersFullUnguarded)
}

// resweepFull is the full resweep behind ResweepWinners and its guarded
// variants; guard decides what the contract re-read under the lock stops.
func (s SupplyChainImpactWinnersStore) resweepFull(
	ctx context.Context,
	guard winnersFullGuard,
) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	var none supplychaincore.SupplyChainImpactWinnersResweep
	if s.DB == nil {
		return none, fmt.Errorf("supply chain impact winners database is required")
	}
	beginner, ok := s.DB.(db.Beginner)
	if !ok {
		return none, fmt.Errorf("supply chain impact winners database must support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin supply chain impact winners resweep: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// The order below needs a fresh snapshot per statement, whatever the
	// session default is: under REPEATABLE READ the rebuild would read the
	// claim's snapshot, miss marks committed after the drain, and stamp S
	// over them.
	if _, err := tx.ExecContext(ctx, pinWinnersFullReadCommittedSQL); err != nil {
		return none, fmt.Errorf("pin supply chain impact winners resweep isolation: %w", err)
	}

	claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx)
	if err != nil {
		return none, err
	}
	if !claimed {
		return supplychaincore.SupplyChainImpactWinnersResweep{Skipped: true}, nil
	}
	if guard != winnersFullUnguarded {
		stored, err := readWinnersContract(ctx, tx)
		if err != nil {
			return none, err
		}
		if stop, ok := guard.stopsOn(stored); ok {
			return stop, nil
		}
	}
	stamp, err := scanWinnersTime(ctx, tx, selectWinnersKeyedTickClockSQL, "read supply chain impact winners full resweep clock")
	if err != nil {
		return none, err
	}
	tally, err := drainWinnersDirtyForFull(ctx, tx)
	if err != nil {
		return none, err
	}
	result, err := s.runSupplyChainImpactWinnersRebuild(ctx, tx, stamp, s.chunkBounds())
	if err != nil {
		return none, err
	}
	result.Path = telemetry.SupplyChainImpactWinnersPathFull
	result.Identifiers = tally.total()
	result.TriggerCaptured = tally.triggerSources()
	if result.Remaining, result.RemainingUpperBound, err = countWinnersFullResidual(ctx, tx); err != nil {
		return none, err
	}
	if result.Watermark, err = scanWinnersTime(ctx, tx, selectWinnersWatermarkSQL, "read committed supply chain impact winners watermark"); err != nil {
		return none, err
	}
	if err := stampWinnersContract(ctx, tx); err != nil {
		return none, err
	}
	// #10876: the resweep commit is the providing save for the
	// winners_watermark wait kind. The wake runs in the same transaction and
	// carries the committed watermark, not the stamp ceiling; the waiting set
	// is narrow (advisory only) and the maintainer commits on a 30-second
	// cadence, inside the measured coarse-wake budget.
	wakeCtx, deferredRequeues := deferWakeRequeues(ctx) // #11624: count at commit.
	if _, err := wakeWinnersWatermark(wakeCtx, tx, s.Instruments, WaitingWakeSiteInTx,
		result.Watermark, time.Now().UTC()); err != nil {
		return none, fmt.Errorf("wake winners waiting items: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return none, fmt.Errorf("commit supply chain impact winners resweep: %w", err)
	}
	deferredRequeues.flush(ctx, s.Instruments)
	return result, nil
}

func claimSupplyChainImpactWinnersResweep(ctx context.Context, tx db.Transaction) (bool, error) {
	rows, err := tx.QueryContext(ctx, tryClaimSupplyChainImpactWinnersResweepSQL)
	if err != nil {
		return false, fmt.Errorf("claim supply chain impact winners resweep: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var claimed bool
	if rows.Next() {
		if err := rows.Scan(&claimed); err != nil {
			return false, fmt.Errorf("scan supply chain impact winners resweep claim: %w", err)
		}
	} else if err := rows.Err(); err != nil {
		return false, fmt.Errorf("claim supply chain impact winners resweep: %w", err)
	} else {
		return false, fmt.Errorf("claim supply chain impact winners resweep: statement returned no row")
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("claim supply chain impact winners resweep: %w", err)
	}
	return claimed, nil
}

// drainWinnersDirtyForFull deletes the dirty-log and staged marks the full
// resweep covers, log first, and returns their marks by capture source. It
// must run after the clock read and before the rebuild statement, in the
// resweep transaction.
func drainWinnersDirtyForFull(ctx context.Context, tx db.Transaction) (winnersCaptureTally, error) {
	tally := winnersCaptureTally{}
	for _, drain := range []struct{ name, sql string }{
		{"dirty log", drainWinnersDirtyLogForFullSQL},
		{"staging", drainWinnersStagingForFullSQL},
	} {
		rows, err := tx.QueryContext(ctx, drain.sql)
		if err != nil {
			return nil, fmt.Errorf("drain supply chain impact winners %s for the full resweep: %w", drain.name, err)
		}
		for rows.Next() {
			var source string
			var n int64
			if err := rows.Scan(&source, &n); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan drained supply chain impact winners %s marks: %w", drain.name, err)
			}
			tally[source] += n
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("drain supply chain impact winners %s for the full resweep: %w", drain.name, err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close drained supply chain impact winners %s marks: %w", drain.name, err)
		}
	}
	return tally, nil
}

// countWinnersFullResidual counts the dirty-log and staged marks left after
// the full resweep's rebuild: marks that committed after the drain. The
// stamp is already capped under them; the count only reports them. The bool
// reports that the staged part is an upper bound past the exact-count cap
// (#11129).
func countWinnersFullResidual(ctx context.Context, tx db.Transaction) (int64, bool, error) {
	logRows, err := scanWinnersInt64(ctx, tx, countWinnersKeyedRemainingSQL, "count supply chain impact winners full residual")
	if err != nil {
		return 0, false, err
	}
	staged, err := countWinnersStagingResidual(ctx, tx, "count supply chain impact winners full staging residual")
	if err != nil {
		return 0, false, err
	}
	return logRows + staged.count, staged.upperBound, nil
}

// runSupplyChainImpactWinnersRebuild walks the full rebuild in bounded chunks
// and stamps the watermark once, after the last chunk. It closes every cursor
// before returning: a statement error can arrive after the row, and the driver
// must be done with the cursor before the transaction commits.
//
// The pre-stamp watermark read comes before the first chunk statement, so the
// reported previous watermark can never observe a chunk's own write (#11515).
// The final stamp's RETURNING row is read for arity only; the caller re-reads
// the committed watermark after GREATEST resolves it.
func (s SupplyChainImpactWinnersStore) runSupplyChainImpactWinnersRebuild(
	ctx context.Context,
	tx db.Transaction,
	stamp time.Time,
	bounds winnersChunkBounds,
) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	var result supplychaincore.SupplyChainImpactWinnersResweep
	previous, err := readWinnersPreStampWatermark(ctx, tx)
	if err != nil {
		return result, err
	}
	ready := fullChunkSupportsProbeReady(ctx, tx, s.Instruments)
	inserted, updated, deleted, unchanged, err := runWinnersFullChunks(ctx, tx, stamp, bounds, ready)
	if err != nil {
		return result, err
	}
	result.Inserted, result.Updated, result.Deleted, result.Unchanged = inserted, updated, deleted, unchanged
	if err := runWinnersFullStamp(ctx, tx, stamp); err != nil {
		return result, err
	}
	result.PreviousWatermark = previous
	// #11466: claim the enrichment first-carry stamps for the reconciled
	// winners in this same transaction, stamped with the rebuild's own S
	// so a newly carried value shares its row version's materialized_at.
	// A claim failure fails the resweep: an unstamped winner would
	// silently drop out of the arrival-to-rescore cohort.
	if err := claimSupplyChainImpactWinnersEnrichmentFirstCarry(ctx, tx, false, stamp); err != nil {
		return result, err
	}
	return result, nil
}

// Watermark returns the committed read watermark. ok is false until the first
// resweep commits.
func (s SupplyChainImpactWinnersStore) Watermark(ctx context.Context) (at time.Time, ok bool, err error) {
	if s.DB == nil {
		return time.Time{}, false, fmt.Errorf("supply chain impact winners database is required")
	}
	rows, err := s.DB.QueryContext(ctx, selectSupplyChainImpactWinnersWatermarkSQL)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read supply chain impact winners watermark: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return time.Time{}, false, rows.Err()
	}
	if err := rows.Scan(&at); err != nil {
		return time.Time{}, false, fmt.Errorf("scan supply chain impact winners watermark: %w", err)
	}
	return at.UTC(), true, rows.Err()
}

// RebuildAllWinners runs ResweepWinners and discards the row counts. It keeps
// the callers that only need the table reconciled, such as backfills and proofs.
// It returns an error when ResweepWinners reports Skipped, because another
// resweep held the single-flight lock and nothing was reconciled or stamped. A
// caller that must have the table reconciled then retries once that resweep
// ends; a nil return always means this call rebuilt the table.
func (s SupplyChainImpactWinnersStore) RebuildAllWinners(ctx context.Context) error {
	result, err := s.ResweepWinners(ctx)
	if err != nil {
		return err
	}
	if result.Skipped {
		return fmt.Errorf("rebuild supply chain impact winners: another resweep is in flight")
	}
	return nil
}

var (
	_ supplychaincore.SupplyChainImpactWinnersRebuilder       = SupplyChainImpactWinnersStore{}
	_ supplychaincore.SupplyChainImpactWinnersWatermarkReader = SupplyChainImpactWinnersStore{}
)
