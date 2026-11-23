// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// holderMassProbeQueryTemplate estimates, per lookup id, how many live
// finding rows name that id, so the reducer can isolate hot ids into their
// own holder-read statement (#10643). All three arms (cve_id, advisory_id,
// group-members) count through a per-id LATERAL over LIMIT K+1 against the
// same partial index the shipped holder read uses (literal fact kind plus
// the tombstone predicate prove the predicate), so the probe avoids the
// scope join and DISTINCT sort of a second full read; the uncapped shape
// measured 6067 ms cold / 218 ms warm at production mass (see the evidence
// note), bounded by the same guard with fail-closed errors. Each arm
// reports the probe cap above K rows and stays exact at or under K. The
// member-arm ON gate excludes its contribution once the filled cheap arms
// already reach the cap, and LEAST caps the total; the heap-fetch bound
// comes from the per-id LIMITs, so worst-case probe work is five chunk ids
// times three arms times K+1 rows, independent of true mass. A cheap-arm
// count in (K, threshold] saturates to the cap (arbiter Shape-B ruling on
// the live estimate timeouts): those ids read hot and run singleton holder
// reads instead of packing, which only splits more, never less. The LEAST,
// saturation, and already-hot short-circuit verbs take
// supplychaincore.HolderMassProbeCap and every fill-to-cap threshold and
// LIMIT (threshold + 1) takes supplychaincore.HolderMassProbeFillToCapRows,
// all through buildHolderMassProbeQuery, never a duplicated literal.
// The probe deliberately overestimates: it counts rows in superseded
// generations (no active-generation join) and counts a row once per arm it
// matches, and an id whose row matches through two ids in one chunk counts
// toward both, plus the fill-to-cap saturates past-K rows to the cap. All
// biases point at splitting, which is the safe direction — an extra
// statement stays far under the guard, while a missing split could re-form
// the timed-out shape. The outer LEFT JOINs report every requested id with
// zero for no match, so the planner only treats an id as unmeasured when
// the probe itself misbehaves. It runs under the same statement timeout as
// the holder read through readIDMasses.
const holderMassProbeQueryTemplate = `
SELECT ids.id AS lookup_id, LEAST(COALESCE(cve.mass, 0) + COALESCE(adv.mass, 0) + COALESCE(m3.mass, 0), %d) AS mass
FROM UNNEST($1::text[]) AS ids(id)
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > %d THEN %d ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.cve_id <> ''
          AND support.cve_id = ids.id
        LIMIT %d
    ) AS limited
) AS cve ON TRUE
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > %d THEN %d ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.advisory_id <> ''
          AND support.advisory_id = ids.id
        LIMIT %d
    ) AS limited
) AS adv ON TRUE
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > %d THEN %d ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.payload #> '{provenance,advisory_group_members}' ? ids.id
        LIMIT %d
    ) AS limited
) AS m3 ON (COALESCE(cve.mass, 0) + COALESCE(adv.mass, 0)) < %d
`

// buildHolderMassProbeQuery renders the probe from its two single sources:
// the LEAST total, every saturation value, and the already-hot
// short-circuit take supplychaincore.HolderMassProbeCap, while every
// fill-to-cap threshold and arm LIMIT (threshold + 1) takes
// supplychaincore.HolderMassProbeFillToCapRows, so a threshold or sample
// change moves every site together.
func buildHolderMassProbeQuery() string {
	probeCap := supplychaincore.HolderMassProbeCap
	fillRows := supplychaincore.HolderMassProbeFillToCapRows
	return fmt.Sprintf(holderMassProbeQueryTemplate,
		probeCap,
		fillRows, probeCap, fillRows+1,
		fillRows, probeCap, fillRows+1,
		fillRows, probeCap, fillRows+1,
		probeCap)
}

// HolderMassProbeQuery is the shipped probe text behind EstimateHolderMasses,
// exported so live proofs EXPLAIN the exact statement production runs rather
// than a hand-copied differential that could drift false-green.
var HolderMassProbeQuery = buildHolderMassProbeQuery()

// EstimateHolderMasses implements supplychaincore.HolderMassEstimator: one row
// per requested lookup id with its live finding-row count capped at
// supplychaincore.HolderMassProbeCap, under the same bounded statement
// timeout as the holder read. A timeout classifies as
// supplychaincore.ErrFanOutReadTimeout exactly like the holder read, so the
// pass fails (and retries) instead of splitting blind.
func (s SupplyChainImpactFanOutStore) EstimateHolderMasses(
	ctx context.Context,
	lookupIDs []string,
) (map[string]int64, error) {
	// A blank id names no finding; it keeps mass 0 (readIDMasses fills every
	// requested id), and sending it would defeat the partial indexes.
	probed := nonBlankLookupIDs(lookupIDs)
	if len(lookupIDs) == 0 {
		return map[string]int64{}, nil
	}
	masses := make(map[string]int64, len(lookupIDs))
	if len(probed) > 0 {
		read, err := s.readIDMasses(ctx, HolderMassProbeQuery, probed)
		if err != nil {
			return nil, err
		}
		masses = read
	}
	for _, id := range lookupIDs {
		if _, ok := masses[id]; !ok {
			masses[id] = 0
		}
	}
	s.Instruments.RecordSupplyChainImpactSupportsRead(ctx, telemetry.SupplyChainImpactSupportsReaderFanoutHolderMass, telemetry.SupplyChainImpactSupportsReadServed)
	return masses, nil
}

// setHolderMassProbeNoSeqScanQuery scopes enable_seqscan = off to the mass-probe
// transaction (arbiter-b2b-gin-covering decision 3). The probe asks each id for
// "at most 101 rows" through a per-id LIMIT. For an id the planner knows nothing
// about it assumes about 1% of the table matches, prices a sequential scan under
// LIMIT at the cost of reading roughly 10,000 rows (about 1,600 at 8,000,000
// supports) and compares that with a bitmap scan whose cost grows with the table.
// A sequential scan wins that race whenever the GIN scan costs more than the
// constant: with an unflushed pending list, or on a table a few times larger
// than the lab's, and a probe for an id that matches nothing then reads the whole
// table (254 s for five ids at 8,000,000 supports). The setting applies to this
// transaction only: the holder and key-range statements keep the planner's own
// choice, which arbiter-hotkey-page decision 10 pins.
const setHolderMassProbeNoSeqScanQuery = `SELECT set_config('enable_seqscan', 'off', true)`

// ApplyHolderMassProbeSettings sets the statement timeout and the mass probe's
// planner setting in the current transaction. readIDMasses calls it; the plan
// guard calls the same helper so the proof runs under exactly the settings
// production applies.
func ApplyHolderMassProbeSettings(ctx context.Context, tx db.Queryer, timeout time.Duration) error {
	rows, err := tx.QueryContext(ctx, setSupplyChainImpactFanOutReadTimeoutQuery, fmt.Sprintf("%dms", timeout.Milliseconds()))
	if err != nil {
		return fmt.Errorf("set supply chain impact holder mass read timeout: %w", err)
	}
	_ = rows.Close()
	rows, err = tx.QueryContext(ctx, setHolderMassProbeNoSeqScanQuery)
	if err != nil {
		return fmt.Errorf("set supply chain impact holder mass read planner setting: %w", err)
	}
	_ = rows.Close()
	return nil
}

// readIDMasses runs one mass-probe statement as a short read-only transaction
// under the bounded statement timeout, mirroring readScopeIDs. Every mass
// probe goes through it, so each has the same timeout, the same typed
// timeout error, and the same retry semantics as the holder read.
func (s SupplyChainImpactFanOutStore) readIDMasses(ctx context.Context, query string, lookupIDs []string) (map[string]int64, error) {
	if s.Beginner == nil {
		return nil, errors.New("supply chain impact fan-out beginner is required")
	}
	timeout := s.ReadTimeout
	if timeout <= 0 {
		timeout = supplyChainImpactFanOutReadTimeout
	}
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin supply chain impact holder mass read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return nil, fmt.Errorf("set supply chain impact holder mass read read-only: %w", err)
	}
	if err := ApplyHolderMassProbeSettings(ctx, tx, timeout); err != nil {
		return nil, err
	}
	if err := requireSupplyChainImpactSupportsReady(ctx, tx, s.Instruments, telemetry.SupplyChainImpactSupportsReaderFanoutHolderMass); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, query, lookupIDs)
	if err != nil {
		return nil, classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	masses := make(map[string]int64, len(lookupIDs))
	for rows.Next() {
		var id string
		var mass int64
		if err := rows.Scan(&id, &mass); err != nil {
			return nil, fmt.Errorf("scan supply chain impact holder mass: %w", err)
		}
		masses[id] = mass
	}
	if err := rows.Err(); err != nil {
		return nil, classifyFanOutReadError(err)
	}
	for _, id := range lookupIDs {
		if _, ok := masses[id]; !ok {
			masses[id] = 0
		}
	}
	return masses, nil
}

// The production store always satisfies the split probe seam: a future
// decorator that stops forwarding EstimateHolderMasses restores the timed-out
// shape silently, so this fails the wiring test instead (#10643 F-3).
var _ supplychaincore.HolderMassEstimator = SupplyChainImpactFanOutStore{}
