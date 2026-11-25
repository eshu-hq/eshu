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
)

// Defaults of the keyed resweep's fallback to the full rebuild (#9805). They are
// the ruling's starting values, not tuned numbers: a keyed tick that would
// recompute more than MaxIdentifiers identifiers, or more than half the stored
// winners, is no cheaper than the full rebuild and runs it instead.
//
// TEMPORARY (#10972): MaxIdentifiers is squeezed to
// 5,000 (from 50,000) so the current ~6,400-identifier expansions trip the
// over-cap guard and every tick takes the existing guarded full path until
// the backlog drains. Revert immediately after the drain; the revert restores
// this constant and its comment together.
const (
	defaultWinnersKeyedMaxIdentifiers = 5000
	defaultWinnersKeyedMaxKeyFraction = 0.5
)

// ResweepWinnersKeyed consumes the dirty log and recomputes only the canonical
// keys it names, in one transaction under the same single-flight lock as the
// full resweep. It writes a row version only for a winner that is new or
// changed and deletes stored winners of the keys that are no longer produced.
//
// The transaction is chunk-bounded (#9805 chunked consume): it admits the
// oldest ChunkMaxDirtyRows dirty rows as candidates, probes at most
// ChunkMaxIdentifiers post-expansion identifiers through the 291 five-tuple
// probe, then deletes exactly the dirty rows it applied — claim and consume
// stay atomic per chunk, and a rollback changes nothing. Every pass stamps
// the watermark (#10706 horizon): the tick stamp S when the log drained, or
// H = min(captured_at) of the remainder minus 1us when rows are still
// queued. The keyed and idle stamps resolve GREATEST(previous, candidate)
// so the watermark never regresses; the full fallback recomputes every key
// and stamps S. S is the DB clock read once before admission, in the same
// domain as captured_at. A row that both executes and commits after the
// tick clock was read is never covered by the stamp; a row that executed
// before it but commits after the tick's snapshot stays covered for exactly
// one tick and the next tick applies it (see the late-commit tests). The
// result reports the re-read committed watermark on every path. A bounded
// pass reports Bounded with the remaining depth. Passes run
// one per maintainer tick (re-ticked immediately while Bounded), so a
// backlog drains as short spaced ticks, never as one long transaction. When
// the admitted window's identifiers exceed MaxIdentifiers, or MaxKeyFraction
// of the stored winners, the same transaction deletes every queued row, runs
// the full rebuild instead, and reports Path "full".
//
// When another resweep is in flight it consumes nothing and returns Skipped.
func (s SupplyChainImpactWinnersStore) ResweepWinnersKeyed(
	ctx context.Context,
) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	var none supplychaincore.SupplyChainImpactWinnersResweep
	if s.DB == nil {
		return none, fmt.Errorf("supply chain impact winners database is required")
	}
	beginner, ok := s.DB.(db.Beginner)
	if !ok {
		return none, fmt.Errorf("supply chain impact winners database must support transactions")
	}
	// claim covers the begin and the try-lock, the two round trips before the
	// tick owns the lock. The rollback is registered as soon as a transaction
	// exists, on the claim's failure path too.
	var tx db.Transaction
	var claimed bool
	err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseClaim, func() error {
		var err error
		if tx, err = beginner.Begin(ctx); err != nil {
			tx = nil
			return fmt.Errorf("begin supply chain impact winners keyed resweep: %w", err)
		}
		claimed, err = claimSupplyChainImpactWinnersResweep(ctx, tx)
		return err
	})
	if tx != nil {
		defer func() { _ = tx.Rollback() }()
	}
	if err != nil {
		return none, err
	}
	if !claimed {
		return supplychaincore.SupplyChainImpactWinnersResweep{Skipped: true}, nil
	}
	// #11117: the maintainer decided this tick from a contract read taken
	// before the lease claim. A newer binary's full can commit in between,
	// so the stored version is re-read under the lock: above this binary's,
	// the tick consumes, applies and stamps nothing, which also keeps the
	// over-cap full fallback below from rebuilding with this binary's
	// statements over a table stamped with the newer contract.
	stored, err := readWinnersContract(ctx, tx)
	if err != nil {
		return none, err
	}
	if stored.HasVersion && stored.StoredVersion > stored.BinaryVersion {
		return supplychaincore.SupplyChainImpactWinnersResweep{Skipped: true, StoodDown: true}, nil
	}
	bounds := s.chunkBounds()
	// The staging collapse runs under the resweep lock before the tick-clock
	// read (#10837 direction C): it folds at most one collapse budget of
	// staged marks into the versioned log with their version math before
	// admission snapshots the window (#11129). The union below covers the
	// rows the bounded collapse left behind: post-bound rows, below-lo late
	// commits, and rows that arrived mid-tick. A rollback restores the
	// staging deletes with the tick.
	var collapsed winnersStagingCollapseOutcome
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseCollapse, func() error {
		var err error
		collapsed, err = collapseWinnersStaging(ctx, tx, int64(s.StagingCollapseMaxRows))
		return err
	}); err != nil {
		return none, err
	}
	s.Instruments.RecordSupplyChainImpactWinnersStagingCollapsed(ctx, collapsed.rows)
	// The tick stamp is read once, before admission (#10706): a row that
	// both executes and commits after this read carries a later captured_at,
	// so stamping S never covers it. captured_at is taken at INSERT, not at
	// commit, so a row that executed before this read and commits after the
	// residual read is covered by S for one tick while still queued; the next
	// tick applies it (the one-tick over-claim the late-commit tests pin).
	// Every branch below stamps this same S when its residual set is empty.
	var tickStamp time.Time
	var admitted int64
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseConsume, func() error {
		var err error
		if tickStamp, err = scanWinnersTime(ctx, tx, selectWinnersKeyedTickClockSQL, "read supply chain impact winners tick clock"); err != nil {
			return err
		}
		admitted, err = admitWinnersDirtyWindow(ctx, tx, bounds.dirtyRows)
		return err
	}); err != nil {
		return none, err
	}
	result := supplychaincore.SupplyChainImpactWinnersResweep{
		Path: telemetry.SupplyChainImpactWinnersPathKeyed,
	}
	switch admitted {
	case 0:
		if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseStamp, func() error {
			// The window was empty at admission; rows that landed while the
			// tick ran are still counted now, so an idle tick never stamps
			// over them: with an empty log it stamps the pre-admission S,
			// else the horizon H over the rows that arrived mid-tick. The
			// residual unions the staging queue (#10837 direction C): the
			// collapse is bounded to one budget per tick, so the staged
			// rows left are its post-bound and below-lo leftovers plus
			// the mid-tick arrivals, and the horizon must cover them all.
			remaining, err := scanWinnersInt64(ctx, tx, countWinnersKeyedRemainingSQL, "count remaining dirty rows")
			if err != nil {
				return err
			}
			var remainingMin sql.NullTime
			if remainingMin, err = selectWinnersRemainingMin(ctx, tx); err != nil {
				return err
			}
			staged, err := countWinnersStagingResidual(ctx, tx, "count remaining staging rows")
			if err != nil {
				return err
			}
			remaining, remainingMin = stagingUnionRemaining(remaining, remainingMin, staged)
			result.Remaining, result.RemainingUpperBound = remaining, staged.upperBound
			var candidate time.Time
			if remaining > 0 {
				result.Bounded = true
				if candidate, err = horizonCandidate(remainingMin); err != nil {
					return err
				}
			} else {
				candidate = tickStamp
			}
			previous, err := stampWinnersWatermark(ctx, tx, candidate)
			if err != nil {
				return err
			}
			result.PreviousWatermark = previous
			watermark, err := scanWinnersTime(ctx, tx, selectWinnersWatermarkSQL, "read committed supply chain impact winners watermark")
			if err != nil {
				return err
			}
			result.Watermark = watermark
			return nil
		}); err != nil {
			return none, err
		}
	default:
		fallback, err := s.runKeyedOrFull(ctx, tx, bounds, tickStamp, &result)
		if err != nil {
			return none, err
		}
		if fallback {
			result.Path = telemetry.SupplyChainImpactWinnersPathFull
		}
	}
	// #10876: the tick's watermark-advance save is the providing save for the
	// winners_watermark wait kind, so the wake runs in this same transaction,
	// after every stamp site (idle S, keyed S-or-horizon, fallback full) and
	// before the commit: the stamp the wake carries is the committed
	// watermark the result re-read on every path. The waiting set is narrow
	// (advisory only) and the maintainer commits on a 30-second cadence, so
	// the coarse range scan stays inside the provider transaction. A tick that
	// lost the claim consumed and stamped nothing and returns before this
	// point, so there is never a wake without its stamp.
	if err := s.wakeWinnersWaits(ctx, tx, result.Watermark); err != nil {
		return none, err
	}
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseCommit, tx.Commit); err != nil {
		return none, fmt.Errorf("commit supply chain impact winners keyed resweep: %w", err)
	}
	return result, nil
}

// wakeWinnersWaits reopens the advisory waits the tick's committed watermark
// covers, in the tick's transaction. It pins the save's lock wait first
// (#7597: lock_timeout 2s) and maps lock_timeout/deadlock aborts to the
// retryable class-less contention error, so a contended tick rolls back and
// re-ticks on the maintainer's cadence instead of wedging behind a parked
// consumer's Fail. A zero watermark means no stamp committed and wakes
// nothing.
func (s SupplyChainImpactWinnersStore) wakeWinnersWaits(
	ctx context.Context,
	tx db.Transaction,
	watermark time.Time,
) error {
	if watermark.IsZero() {
		return nil
	}
	if _, err := tx.ExecContext(ctx, winnersWakeLockTimeoutSQL,
		fmt.Sprintf("%dms", defaultWinnersWakeLockTimeout.Milliseconds())); err != nil {
		return classifyWinnersWakeError(fmt.Errorf("pin winners wake lock timeout: %w", err))
	}
	if _, err := wakeWinnersWatermark(ctx, tx, s.Instruments, WaitingWakeSiteInTx,
		watermark.UTC(), time.Now().UTC()); err != nil {
		return classifyWinnersWakeError(err)
	}
	return nil
}

// runKeyedOrFull expands the admitted window to identifiers, applies the cap,
// and runs either the bounded keyed apply or the full rebuild. It reports
// whether it fell back to the full rebuild. tickStamp is the DB-clock stamp
// the tick read before admission (#10706): every branch that stamps S stamps
// this same S, never a later re-read.
func (s SupplyChainImpactWinnersStore) runKeyedOrFull(
	ctx context.Context,
	tx db.Transaction,
	bounds winnersChunkBounds,
	tickStamp time.Time,
	result *supplychaincore.SupplyChainImpactWinnersResweep,
) (fellBack bool, err error) {
	// The identifier set is built, and the cap checked, in this order on purpose:
	// the cap counts distinct identifiers, which exist only after the scope rows
	// are expanded. The expansion covers only the admitted dirty-row window,
	// so a backlog of scope rows (bootstrap seeding, a reducer outage) drains
	// over successive ticks instead of materializing every impact identifier of
	// those generations at once; each tick's expansion is bounded by the window
	// and read through the active-scan index.
	var over bool
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseBuildIdents, func() error {
		for _, stmt := range winnersKeyedBuildIdentsStmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("build supply chain impact winners keyed identifiers: %w", err)
			}
		}
		idents, err := scanWinnersInt64(ctx, tx, countWinnersKeyedIdentsSQL, "count keyed identifiers")
		if err != nil {
			return err
		}
		over, err = s.overKeyedCap(ctx, tx, idents)
		return err
	}); err != nil {
		return false, err
	}
	if over {
		if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseFullRebuild, func() error {
			tally, remaining, remainingMin, upperBound, err := settleWinnersKeyedDirtyLog(ctx, tx, true, nil)
			if err != nil {
				return err
			}
			result.Identifiers = tally.total()
			result.TriggerCaptured = tally.triggerSources()
			result.Remaining, result.RemainingUpperBound = remaining, upperBound
			// The drain only deletes admitted rows whose version still
			// matches (#10827 R2): rows that arrived after admission were
			// never admitted, and re-marked keys keep their bumped version
			// for the next tick. When the residual set is non-empty the
			// tick stamps the horizon over it like the keyed path, not S:
			// stamping S would cover residuals the rebuild may not have
			// seen. A residual-free fallback stamps the pre-admission S, a
			// full rebuild recomputes every key.
			stamp := tickStamp
			if remaining > 0 {
				result.Bounded = true
				if stamp, err = horizonCandidate(remainingMin); err != nil {
					return err
				}
			}
			full, err := runSupplyChainImpactWinnersRebuild(ctx, tx, stamp)
			if err != nil {
				return err
			}
			result.Inserted, result.Updated, result.Deleted, result.Unchanged = full.Inserted, full.Updated, full.Deleted, full.Unchanged
			result.PreviousWatermark = full.PreviousWatermark
			// Report the re-read committed watermark (#10706 F4), not the
			// stamp input, so the result agrees with the database on every
			// path.
			watermark, err := scanWinnersTime(ctx, tx, selectWinnersWatermarkSQL, "read committed supply chain impact winners watermark")
			if err != nil {
				return err
			}
			result.Watermark = watermark
			return nil
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	var chunk winnersChunkOutcome
	var kept, scanned int64
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseBuildActiveFacts, func() error {
		var err error
		if chunk, err = buildWinnersKeyedChunk(ctx, tx, bounds); err != nil {
			return err
		}
		// The probe is the shipped per-identifier build over the 291
		// five-tuple keyed index, restricted to the chunk by the
		// identifier table above: no per-triple statements, and the
		// scanned-copies count reads the persisted narrow table instead
		// of re-running the probe, so the probe runs once per tick.
		if _, err := tx.ExecContext(ctx, buildWinnersKeyedNarrowSQL); err != nil {
			return fmt.Errorf("build supply chain impact winners keyed narrow probe: %w", err)
		}
		if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedNarrowSQL); err != nil {
			return fmt.Errorf("analyze supply chain impact winners keyed narrow probe: %w", err)
		}
		if _, err := tx.ExecContext(ctx, buildWinnersKeyedActiveFactsSQL); err != nil {
			return fmt.Errorf("build supply chain impact winners keyed active facts: %w", err)
		}
		if kept, err = countWinnersKeyedKeptFacts(ctx, tx); err != nil {
			return err
		}
		// The pre-filter scan count (#11308) rides the same phase: it
		// counts the persisted narrow rows, so the build's phase sample
		// keeps covering both reads while the probe runs once.
		if scanned, err = countWinnersKeyedScannedCopies(ctx, tx); err != nil {
			return err
		}
		for _, stmt := range []string{addWinnersKeyedOperatorFactsSQL, analyzeWinnersKeyedActiveFactsSQL} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("build supply chain impact winners keyed active facts: %w", err)
			}
		}
		return nil
	}); err != nil {
		return false, err
	}
	result.ChunkIdentifiers = chunk.identifiers
	result.ChunkKeptRows = kept
	result.ChunkScannedCopies = scanned
	return false, s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseApply, func() error {
		tally, remaining, remainingMin, upperBound, err := settleWinnersKeyedDirtyLog(ctx, tx, false, &result.ScopeRemainderConverted)
		if err != nil {
			return err
		}
		s.Instruments.RecordSupplyChainImpactWinnersScopeRemainderConverted(ctx, result.ScopeRemainderConverted)
		result.Identifiers = tally.total()
		result.TriggerCaptured = tally.triggerSources()
		result.Remaining, result.RemainingUpperBound = remaining, upperBound
		// Every pass stamps: S when the log drained, else the horizon H over
		// the rows it leaves queued. Bounded stays the capacity signal the
		// maintainer re-ticks on; it no longer gates the stamp. S is the
		// stamp the tick read before admission, never a post-settle re-read.
		result.Bounded = chunk.truncated || remaining > 0
		var candidate time.Time
		if remaining > 0 {
			if candidate, err = horizonCandidate(remainingMin); err != nil {
				return err
			}
		} else {
			candidate = tickStamp
		}
		if err := applyWinnersKeyed(ctx, tx, candidate, result); err != nil {
			return err
		}
		watermark, err := scanWinnersTime(ctx, tx, selectWinnersWatermarkSQL, "read committed supply chain impact winners watermark")
		if err != nil {
			return err
		}
		result.Watermark = watermark
		return nil
	})
}

// countWinnersKeyedScannedCopies counts the pre-filter copies the identifier
// probe scanned, before the active-generation join discards the retired and
// superseded ones (#11308).
func countWinnersKeyedScannedCopies(ctx context.Context, tx db.Transaction) (int64, error) {
	return scanWinnersInt64(ctx, tx, countWinnersKeyedScannedCopiesSQL, "count scanned winner copies")
}

// applyWinnersKeyed runs the keyed apply statement with the stamp candidate
// and reads its one row of counts into result. It drains and closes the
// cursor before returning, so the driver is done with it before the
// transaction commits.
func applyWinnersKeyed(
	ctx context.Context,
	tx db.Transaction,
	candidate time.Time,
	result *supplychaincore.SupplyChainImpactWinnersResweep,
) error {
	rows, err := tx.QueryContext(ctx, applyWinnersKeyedSQL, candidate)
	if err != nil {
		return fmt.Errorf("apply supply chain impact winners keyed resweep: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("apply supply chain impact winners keyed resweep: %w", err)
		}
		return fmt.Errorf("apply supply chain impact winners keyed resweep: statement returned no counts")
	}
	var previous sql.NullTime
	if err := rows.Scan(&result.Inserted, &result.Updated, &result.Deleted, &result.Unchanged, &result.Keys, &previous); err != nil {
		return fmt.Errorf("scan supply chain impact winners keyed counts: %w", err)
	}
	if rows.Next() {
		return fmt.Errorf("apply supply chain impact winners keyed resweep: statement returned more than one row")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("apply supply chain impact winners keyed resweep: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close supply chain impact winners keyed counts: %w", err)
	}
	if previous.Valid {
		result.PreviousWatermark = previous.Time.UTC()
	}
	return nil
}

// timePhase runs fn and records its wall time as one sample of phase on the
// tick-phase histogram, whether fn succeeded or failed (#9975). It adds a timer
// around the statements fn runs and nothing else: no statement, lock or
// ordering changes. With no Instruments it only calls fn.
func (s SupplyChainImpactWinnersStore) timePhase(ctx context.Context, phase string, fn func() error) error {
	started := time.Now()
	err := fn()
	s.Instruments.RecordSupplyChainImpactWinnersTickPhase(ctx, phase, time.Since(started))
	if err != nil {
		s.Instruments.RecordSupplyChainImpactWinnersWriteConflict(ctx, telemetry.SupplyChainImpactWinnersConflictSiteTick, winnersWriteConflictClass(err))
	}
	return err
}

// overKeyedCap reports whether idents is more than MaxIdentifiers or more than
// MaxKeyFraction of the stored winners.
func (s SupplyChainImpactWinnersStore) overKeyedCap(ctx context.Context, tx db.Transaction, idents int64) (bool, error) {
	maxIdents := int64(s.MaxIdentifiers)
	if maxIdents <= 0 {
		maxIdents = defaultWinnersKeyedMaxIdentifiers
	}
	if idents > maxIdents {
		return true, nil
	}
	fraction := s.MaxKeyFraction
	if fraction <= 0 {
		fraction = defaultWinnersKeyedMaxKeyFraction
	}
	stored, err := scanWinnersInt64(ctx, tx, winnersKeyedStoredEstimateSQL, "estimate stored winners")
	if err != nil {
		return false, err
	}
	return stored > 0 && float64(idents) > fraction*float64(stored), nil
}

var (
	_ supplychaincore.SupplyChainImpactWinnersKeyedRebuilder = SupplyChainImpactWinnersStore{}
	_ supplychaincore.SupplyChainImpactWinnersDirtyLogReader = SupplyChainImpactWinnersStore{}
	_ supplychaincore.SupplyChainImpactWinnersCapturer       = (*supplyChainImpactTx)(nil)
)
