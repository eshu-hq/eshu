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
// recompute more than 50,000 identifiers, or more than half the stored winners,
// is no cheaper than the full rebuild and runs it instead.
const (
	defaultWinnersKeyedMaxIdentifiers = 50000
	defaultWinnersKeyedMaxKeyFraction = 0.5
)

// ResweepWinnersKeyed consumes the dirty log and recomputes only the canonical
// keys it names, in one transaction under the same single-flight lock as the
// full resweep. It writes a row version only for a winner that is new or
// changed and deletes stored winners of the keys that are no longer produced.
//
// The transaction is chunk-bounded (#9805 chunked consume): it admits the
// oldest ChunkMaxDirtyRows dirty rows as candidates, probes at most
// ChunkMaxTriples distinct package triples and at most
// ChunkMaxCandidateRows fetched candidate rows, then deletes exactly the
// dirty rows it applied — claim and consume stay atomic per chunk, and a
// rollback changes nothing. A bounded pass leaves the rest queued, reports
// Bounded with the remaining depth, and stamps no watermark; the watermark
// moves only on a pass after which the dirty log is empty. Passes run one
// per maintainer tick, so a backlog drains as short spaced ticks, never as
// one long transaction. When the admitted window's identifiers exceed
// MaxIdentifiers, or MaxKeyFraction of the stored winners, the same
// transaction deletes every queued row, runs the full rebuild instead, and
// reports Path "full".
//
// When another resweep is in flight it consumes nothing and returns Skipped.
func (s SupplyChainImpactWinnersStore) ResweepWinnersKeyed(
	ctx context.Context,
	materializedAt any,
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
	bounds := s.chunkBounds()
	var admitted int64
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseConsume, func() error {
		var err error
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
			// The window was empty at admission; stamp only if the log is
			// still empty now, so an idle tick never stamps over rows that
			// landed while it ran.
			remaining, err := scanWinnersInt64(ctx, tx, countWinnersKeyedRemainingSQL, "count remaining dirty rows")
			if err != nil {
				return err
			}
			result.Remaining = remaining
			if remaining > 0 {
				result.Bounded = true
				return nil
			}
			previous, err := stampWinnersWatermark(ctx, tx, materializedAt)
			result.PreviousWatermark = previous
			return err
		}); err != nil {
			return none, err
		}
	default:
		fallback, err := s.runKeyedOrFull(ctx, tx, materializedAt, bounds, &result)
		if err != nil {
			return none, err
		}
		if fallback {
			result.Path = telemetry.SupplyChainImpactWinnersPathFull
		}
	}
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseCommit, tx.Commit); err != nil {
		return none, fmt.Errorf("commit supply chain impact winners keyed resweep: %w", err)
	}
	return result, nil
}

// runKeyedOrFull expands the admitted window to identifiers, applies the cap,
// and runs either the bounded keyed apply or the full rebuild. It reports
// whether it fell back to the full rebuild.
func (s SupplyChainImpactWinnersStore) runKeyedOrFull(
	ctx context.Context,
	tx db.Transaction,
	materializedAt any,
	bounds winnersChunkBounds,
	result *supplychaincore.SupplyChainImpactWinnersResweep,
) (fellBack bool, err error) {
	// The identifier set is built, and the cap checked, in this order on purpose:
	// the cap counts distinct identifiers, which exist only after the scope rows
	// are expanded. A backlog of scope rows (bootstrap seeding, a reducer outage)
	// therefore materializes every impact identifier of those generations in the
	// temp table once before the tick falls back to the full rebuild. The cost
	// is bounded by the fact set, paid once per backlog, and read through the
	// active-scan index.
	var over bool
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseBuildIdents, func() error {
		for _, stmt := range []string{buildWinnersKeyedScopeIdentsSQL, buildWinnersKeyedIdentsSQL, analyzeWinnersKeyedIdentsSQL} {
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
			tally, remaining, err := settleWinnersKeyedDirtyLog(ctx, tx, true)
			if err != nil {
				return err
			}
			result.Identifiers = tally.total()
			result.TriggerCaptured = tally.triggerSources()
			result.Remaining = remaining
			full, err := runSupplyChainImpactWinnersRebuild(ctx, tx, materializedAt)
			if err != nil {
				return err
			}
			result.Inserted, result.Updated, result.Deleted, result.Unchanged = full.Inserted, full.Updated, full.Deleted, full.Unchanged
			result.PreviousWatermark = full.PreviousWatermark
			return nil
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	var chunk winnersChunkOutcome
	if err := s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseBuildActiveFacts, func() error {
		var err error
		if chunk, err = buildWinnersKeyedChunk(ctx, tx, bounds); err != nil {
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
	result.Triples = chunk.triples
	result.CandidateRows = chunk.candidateRows
	return false, s.timePhase(ctx, telemetry.SupplyChainImpactWinnersPhaseApply, func() error {
		tally, remaining, err := settleWinnersKeyedDirtyLog(ctx, tx, false)
		if err != nil {
			return err
		}
		result.Identifiers = tally.total()
		result.TriggerCaptured = tally.triggerSources()
		result.Remaining = remaining
		// The watermark moves only when this pass applied everything: the
		// chunk was not truncated and the log is empty after the applied
		// rows were deleted (rows that land after this count are newer than
		// the tick's stamp and wait for the next tick, as before).
		result.Bounded = chunk.truncated || remaining > 0
		return applyWinnersKeyed(ctx, tx, materializedAt, result)
	})
}

// applyWinnersKeyed runs the keyed apply statement and reads its one row of
// counts into result. It drains and closes the cursor before returning, so the
// driver is done with it before the transaction commits.
func applyWinnersKeyed(
	ctx context.Context,
	tx db.Transaction,
	materializedAt any,
	result *supplychaincore.SupplyChainImpactWinnersResweep,
) error {
	applySQL := applyWinnersKeyedSQL
	if result.Bounded {
		applySQL = applyWinnersKeyedNoStampSQL
	}
	rows, err := tx.QueryContext(ctx, applySQL, materializedAt)
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

// winnersCaptureTally is the consumed dirty-log rows by capture source.
type winnersCaptureTally map[string]int64

func (t winnersCaptureTally) total() int64 {
	var n int64
	for _, v := range t {
		n += v
	}
	return n
}

// triggerSources returns the consumed rows of the sources a database trigger
// owns, the only ones the maintainer counts at consume.
func (t winnersCaptureTally) triggerSources() supplychaincore.WinnersTriggerCaptured {
	return supplychaincore.WinnersTriggerCaptured{
		Activation: t[telemetry.SupplyChainImpactWinnersSourceActivation],
		Operator:   t[telemetry.SupplyChainImpactWinnersSourceOperator],
		Scanner:    t[telemetry.SupplyChainImpactWinnersSourceScanner],
	}
}

// admitWinnersDirtyWindow copies the oldest limit dirty-log rows into the
// consumed temp table and returns how many it admitted. The rows stay in the
// log until settleWinnersKeyedDirtyLog deletes the applied ones.
func admitWinnersDirtyWindow(ctx context.Context, tx db.Transaction, limit int64) (int64, error) {
	if _, err := tx.ExecContext(ctx, createWinnersKeyedConsumedSQL); err != nil {
		return 0, fmt.Errorf("admit supply chain impact winners dirty window: %w", err)
	}
	if _, err := tx.ExecContext(ctx, admitWinnersKeyedWindowSQL, limit); err != nil {
		return 0, fmt.Errorf("admit supply chain impact winners dirty window: %w", err)
	}
	return scanWinnersInt64(ctx, tx, countWinnersKeyedConsumedSQL, "count admitted dirty rows")
}

// stampWinnersWatermark stamps the watermark for a tick that consumed nothing
// and returns the watermark it replaced.
func stampWinnersWatermark(ctx context.Context, tx db.Transaction, materializedAt any) (time.Time, error) {
	rows, err := tx.QueryContext(ctx, stampWinnersWatermarkSQL, materializedAt)
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

// DirtyLogStats reads the dirty log's depth and the age of its oldest row, for
// the operator gauges. An empty log reads as zero depth and zero age.
func (s SupplyChainImpactWinnersStore) DirtyLogStats(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersDirtyStats, error) {
	var stats supplychaincore.SupplyChainImpactWinnersDirtyStats
	if s.DB == nil {
		return stats, fmt.Errorf("supply chain impact winners database is required")
	}
	rows, err := s.DB.QueryContext(ctx, selectWinnersDirtyLogStatsSQL)
	if err != nil {
		return stats, fmt.Errorf("read supply chain impact winners dirty log stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return stats, rows.Err()
	}
	var ageSeconds float64
	if err := rows.Scan(&stats.Depth, &ageSeconds); err != nil {
		return stats, fmt.Errorf("scan supply chain impact winners dirty log stats: %w", err)
	}
	stats.OldestAge = time.Duration(ageSeconds * float64(time.Second))
	return stats, rows.Err()
}

var (
	_ supplychaincore.SupplyChainImpactWinnersKeyedRebuilder = SupplyChainImpactWinnersStore{}
	_ supplychaincore.SupplyChainImpactWinnersDirtyLogReader = SupplyChainImpactWinnersStore{}
	_ supplychaincore.SupplyChainImpactWinnersCapturer       = (*supplyChainImpactTx)(nil)
)
