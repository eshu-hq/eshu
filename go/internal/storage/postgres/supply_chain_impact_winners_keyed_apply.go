// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// pinWinnersResweepStatementTimeoutSQL pins the Dec 7 statement guard for
// the tick transaction: every statement past the timeout aborts with
// SQLSTATE 57014, which the tick classifies as statement_timeout.
const pinWinnersResweepStatementTimeoutSQL = `SELECT set_config('statement_timeout', $1, true)`

// resetWinnersResweepStatementTimeoutSQL lifts the Dec 7 guard for the
// over-cap full fallback inside the same transaction: a full recompute's
// legitimate duration is unbounded by the tick bar, so the guard covers
// keyed statements only.
const resetWinnersResweepStatementTimeoutSQL = `RESET statement_timeout`

// resweepStatementTimeout resolves the Dec 7 statement guard: the configured
// ResweepStatementTimeout, or the default sized from the post-fix tick
// maximum.
func (s SupplyChainImpactWinnersStore) resweepStatementTimeout() time.Duration {
	if s.ResweepStatementTimeout <= 0 {
		return defaultWinnersResweepStatementTimeout
	}
	return s.ResweepStatementTimeout
}

// pinResweepStatementTimeout pins the Dec 7 statement guard on the tick
// transaction.
func (s SupplyChainImpactWinnersStore) pinResweepStatementTimeout(ctx context.Context, tx db.Transaction) error {
	timeout := s.resweepStatementTimeout()
	if _, err := tx.ExecContext(ctx, pinWinnersResweepStatementTimeoutSQL, formatWinnersResweepTimeout(timeout)); err != nil {
		return fmt.Errorf("pin supply chain impact winners resweep statement timeout: %w", err)
	}
	return nil
}

// formatWinnersResweepTimeout renders a timeout for Postgres set_config,
// following the winners wake lock_timeout precedent.
func formatWinnersResweepTimeout(timeout time.Duration) string {
	return fmt.Sprintf("%dms", timeout.Milliseconds())
}

// winnersResweepTimeoutClass returns the distinct failure class for a
// server-side statement-timeout abort (SQLSTATE 57014: the Dec 7 guard
// tripping), or "" when err is nil or another failure. It complements
// winnersWriteConflictClass (deadlock/serialization_failure): the tick
// records whichever classifies, so the guard trips observably instead of
// hiding inside unclassified errors.
func winnersResweepTimeoutClass(err error) string {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) {
		return ""
	}
	if pgErr.Code == "57014" {
		return "statement_timeout"
	}
	return ""
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
			// The full recompute's legitimate duration is unbounded by
			// the tick bar, so the Dec 7 keyed-statement guard lifts for
			// this branch: a guarded full would abort a legitimate
			// bootstrap-scale rebuild instead of bounding a runaway tick.
			if _, err := tx.ExecContext(ctx, resetWinnersResweepStatementTimeoutSQL); err != nil {
				return fmt.Errorf("lift supply chain impact winners resweep statement timeout: %w", err)
			}
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
		// The budgeted admission above probed the admitted prefix one
		// identifier at a time into the persisted narrow table (the same
		// supports-swapped probe body the whole-chunk build runs,
		// staged to the batch identifier table), so the probe ran once
		// per admitted identifier and the scanned-copies count below reads
		// the persisted rows instead of re-running it. No wholesale
		// narrow build runs here: the table is already populated, and a
		// second CREATE would fail. Analyze plans the final fetch on real
		// statistics.
		if _, err := tx.ExecContext(ctx, analyzeWinnersKeyedNarrowSQL); err != nil {
			return fmt.Errorf("analyze supply chain impact winners keyed narrow probe: %w", err)
		}
		if _, err := tx.ExecContext(ctx, buildWinnersKeyedActiveFactsFromSupportsSQL); err != nil {
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
		for _, stmt := range []string{addWinnersKeyedOperatorFactsFromSupportsSQL, analyzeWinnersKeyedActiveFactsSQL} {
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
	result.ChunkTruncatedBy = winnersChunkTruncatedBy(chunk)
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
		// maintainer re-ticks on; it no longer gates the stamp. Either
		// admission bound stops the tick the same way. S is the
		// stamp the tick read before admission, never a post-settle re-read.
		result.Bounded = chunk.truncated || chunk.budgetTruncated || remaining > 0
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
