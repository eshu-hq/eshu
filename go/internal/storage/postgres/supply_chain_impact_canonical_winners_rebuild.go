// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
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
	"service_ids", "workload_ids", "environments",
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

// rebuildSupplyChainImpactWinnersSQL atomically reconciles the winners table to
// the current active set and stamps the maintainer watermark, in one statement
// (one snapshot), so a concurrent reader never sees a torn rebuild.
//
// It recomputes every winner (winners_now) but writes only the rows that need
// it (#9774). The changed CTE keeps a winner that has no stored row or whose
// data columns differ from the stored row, and the INSERT reads from changed, so
// an unchanged row never reaches the INSERT and is never locked, versioned or
// written to WAL. A plain ON CONFLICT DO UPDATE ... WHERE would not do: it still
// locks every conflicting row, which writes WAL per row.
//
// The change test reads the stored table, so it is only correct against the
// last committed resweep. ResweepWinners runs this statement after winning
// tryClaimSupplyChainImpactWinnersResweepSQL, in a later statement of the same
// transaction, so its snapshot always sees that commit. ON CONFLICT DO UPDATE
// is not a backstop for a resweep that skips the claim: it only covers rows the
// statement chose to write, never a row it judged unchanged against a stale read.
//
// materialized_at is left out of the comparison and is set only when a row is
// written, so on a winner row it means "last content change". Readers do not
// use it: freshness comes from the singleton watermark below, which every
// resweep stamps whether or not a row changed.
//
// The deleted CTE removes rows whose canonical_key is no longer a winner. The
// final watermark upsert is unconditional, so it stamps even a resweep that
// produced zero winners, which lets the read tell "never populated" (no
// watermark row) from "reswept to zero findings" (watermark present, table
// empty). Its RETURNING reports the row counts and the watermark it replaced.
// The data-modifying CTEs always run to completion, whether or not the final
// statement reads them.
//
// $1 is the new watermark and the materialized_at of every written row. Run it
// only through ResweepWinners.
var rebuildSupplyChainImpactWinnersSQL = `
WITH winners_now AS (` + supplyChainImpactWinnerSelectSQL + `),
changed AS MATERIALIZED (
    SELECT n.*, (w.canonical_key IS NULL) AS is_new
    FROM winners_now AS n
    LEFT JOIN supply_chain_impact_canonical_winners AS w
      ON w.canonical_key = n.canonical_key
    WHERE w.canonical_key IS NULL
       OR ` + supplyChainImpactWinnerRowExpr("n") + ` IS DISTINCT FROM ` + supplyChainImpactWinnerRowExpr("w") + `
),
upserted AS (
    INSERT INTO supply_chain_impact_canonical_winners (
        canonical_key, ` + strings.Join(supplyChainImpactWinnerDataColumns, ", ") + `, materialized_at
    )
    SELECT
        canonical_key, ` + strings.Join(supplyChainImpactWinnerDataColumns, ", ") + `, $1
    FROM changed
    ON CONFLICT (canonical_key) DO UPDATE SET
        ` + supplyChainImpactWinnerConflictSet() + `
),
deleted AS (
    DELETE FROM supply_chain_impact_canonical_winners w
    WHERE NOT EXISTS (SELECT 1 FROM winners_now n WHERE n.canonical_key = w.canonical_key)
    RETURNING 1
)
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at
RETURNING
    (SELECT count(*) FROM changed WHERE is_new),
    (SELECT count(*) FROM changed WHERE NOT is_new),
    (SELECT count(*) FROM deleted),
    (SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed),
    (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1)
`

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

// selectSupplyChainImpactWinnersWatermarkSQL reads the singleton watermark row.
const selectSupplyChainImpactWinnersWatermarkSQL = `
SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1
`

// SupplyChainImpactWinnersStore maintains the supply_chain_impact_canonical_winners
// read model from the active impact facts.
type SupplyChainImpactWinnersStore struct {
	DB db.Queryer
	// MaxIdentifiers and MaxKeyFraction cap a keyed resweep (#9805): a tick whose
	// consumed dirty-log rows expand to more identifiers than MaxIdentifiers, or
	// to more than MaxKeyFraction of the stored winners, runs the full rebuild in
	// the same transaction. Zero means the defaults, 50,000 and 0.5.
	MaxIdentifiers int
	MaxKeyFraction float64
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
// impact-fact set in one atomic statement and reports what it wrote. It writes
// a row version only for a winner that is new or changed, deletes winners no
// longer in the active set, and always stamps the watermark. It is the #3389
// backfill and the safe full-resweep backstop; the per-canonical_key
// incremental path used by the shared-projection worker is a future addition
// that reuses supplyChainImpactWinnerSelectSQL with a canonical_key filter.
//
// When another resweep is in flight it writes nothing, stamps nothing and
// returns Skipped. Any other non-nil result means the transaction committed; on
// an error the result is the zero value and nothing was written.
func (s SupplyChainImpactWinnersStore) ResweepWinners(
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
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return none, fmt.Errorf("begin supply chain impact winners resweep: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	claimed, err := claimSupplyChainImpactWinnersResweep(ctx, tx)
	if err != nil {
		return none, err
	}
	if !claimed {
		return supplychaincore.SupplyChainImpactWinnersResweep{Skipped: true}, nil
	}
	result, err := runSupplyChainImpactWinnersRebuild(ctx, tx, materializedAt)
	if err != nil {
		return none, err
	}
	if err := tx.Commit(); err != nil {
		return none, fmt.Errorf("commit supply chain impact winners resweep: %w", err)
	}
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

// runSupplyChainImpactWinnersRebuild runs the rebuild statement and reads its one
// row of counts. It drains and closes the cursor before returning: a statement
// error can arrive after the row, and the driver must be done with the cursor
// before the transaction commits.
func runSupplyChainImpactWinnersRebuild(
	ctx context.Context,
	tx db.Transaction,
	materializedAt any,
) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	var result supplychaincore.SupplyChainImpactWinnersResweep
	rows, err := tx.QueryContext(ctx, rebuildSupplyChainImpactWinnersSQL, materializedAt)
	if err != nil {
		return result, fmt.Errorf("rebuild supply chain impact canonical winners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return result, fmt.Errorf("rebuild supply chain impact canonical winners: %w", err)
		}
		return result, fmt.Errorf("rebuild supply chain impact canonical winners: statement returned no counts")
	}
	var previous sql.NullTime
	if err := rows.Scan(&result.Inserted, &result.Updated, &result.Deleted, &result.Unchanged, &previous); err != nil {
		return result, fmt.Errorf("scan supply chain impact canonical winners counts: %w", err)
	}
	if rows.Next() {
		return result, fmt.Errorf("rebuild supply chain impact canonical winners: statement returned more than one row")
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("rebuild supply chain impact canonical winners: %w", err)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("close supply chain impact canonical winners counts: %w", err)
	}
	if previous.Valid {
		result.PreviousWatermark = previous.Time.UTC()
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
func (s SupplyChainImpactWinnersStore) RebuildAllWinners(ctx context.Context, materializedAt any) error {
	result, err := s.ResweepWinners(ctx, materializedAt)
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
