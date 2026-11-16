// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// holderMassProbeQueryTemplate estimates, per lookup id, how many live
// finding rows name that id, so the reducer can isolate hot ids into their
// own holder-read statement (#10643). The cheap arms (cve_id, advisory_id)
// count exactly against the same partial index the shipped holder read uses
// (literal fact kind plus the tombstone predicate prove the predicate), so
// the probe avoids the scope join and DISTINCT sort of a second full read;
// the uncapped shape measured 6067 ms cold / 218 ms warm at production mass
// (see the evidence note), bounded by the same guard with fail-closed
// errors. The group-members arm (#10681 design B) counts through a per-id
// LATERAL over LIMIT K+1 and reports the probe cap above K rows, exact at or
// under K (S2 theory shape: 98-196 ms cold, far under the 5 s bar). The ON
// gate excludes the member-arm contribution for already-hot ids and LEAST
// caps the total; the heap-fetch bound comes from the per-id LIMIT. A hot id
// still reads hot while the heap fetch stays bounded; at or under K member
// rows every arm is exact, so cold ids pack exactly as before and split
// decisions match the uncapped probe. The LEAST and already-hot
// short-circuit verbs take supplychaincore.HolderMassProbeCap and the
// fill-to-cap verbs take supplychaincore.HolderMassProbeFillToCapRows,
// all through buildHolderMassProbeQuery, never a duplicated literal.
// The cheap arms stay deliberately unbounded and exact: capping them needs
// an arbiter call, so this file must keep exactly one LIMIT (arm-3 only).
// The probe deliberately overestimates: it counts rows in superseded
// generations (no active-generation join) and counts a row once per arm it
// matches, and an id whose row matches through two ids in one chunk counts
// toward both, plus the fill-to-cap saturates a 101st..threshold member row
// to the cap. All four biases point at splitting, which is the safe
// direction — an extra statement stays far under the guard, while a missing
// split could re-form the timed-out shape. The outer LEFT JOINs report every
// requested id with zero for no match, so the planner only treats an id as
// unmeasured when the probe itself misbehaves. It runs under the same
// statement timeout as the holder read through readIDMasses.
const holderMassProbeQueryTemplate = `
SELECT ids.id AS lookup_id, LEAST(COALESCE(cheap.mass, 0) + COALESCE(m3.mass, 0), %d) AS mass
FROM UNNEST($1::text[]) AS ids(id)
LEFT JOIN (
    SELECT lookup_id, COUNT(*) AS mass
    FROM (
        SELECT per_arm.id AS lookup_id
        FROM UNNEST($1::text[]) AS per_arm(id)
        JOIN fact_records AS finding
          ON finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
         AND finding.is_tombstone = FALSE
         AND finding.payload->>'cve_id' = per_arm.id
        UNION ALL
        SELECT per_arm.id AS lookup_id
        FROM UNNEST($1::text[]) AS per_arm(id)
        JOIN fact_records AS finding
          ON finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
         AND finding.is_tombstone = FALSE
         AND finding.payload->>'advisory_id' = per_arm.id
    ) AS cheap_arms
    GROUP BY lookup_id
) AS cheap ON cheap.lookup_id = ids.id
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > %d THEN %d ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM fact_records AS finding
        WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
          AND finding.is_tombstone = FALSE
          AND finding.payload #> '{provenance,advisory_group_members}' ? ids.id
        LIMIT %d
    ) AS limited
) AS m3 ON COALESCE(cheap.mass, 0) < %d
`

// buildHolderMassProbeQuery renders the probe from its two single sources:
// the LEAST total, the fill-to-cap saturation value, and the already-hot
// short-circuit take supplychaincore.HolderMassProbeCap, while the
// fill-to-cap threshold and the member-arm LIMIT (threshold + 1) take
// supplychaincore.HolderMassProbeFillToCapRows, so a threshold or sample
// change moves every site together.
func buildHolderMassProbeQuery() string {
	probeCap := supplychaincore.HolderMassProbeCap
	fillRows := supplychaincore.HolderMassProbeFillToCapRows
	return fmt.Sprintf(holderMassProbeQueryTemplate, probeCap, fillRows, probeCap, fillRows+1, probeCap)
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
	if len(lookupIDs) == 0 {
		return map[string]int64{}, nil
	}
	return s.readIDMasses(ctx, HolderMassProbeQuery, lookupIDs)
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
	rows, err := tx.QueryContext(ctx, setSupplyChainImpactFanOutReadTimeoutQuery, fmt.Sprintf("%dms", timeout.Milliseconds()))
	if err != nil {
		return nil, fmt.Errorf("set supply chain impact holder mass read timeout: %w", err)
	}
	_ = rows.Close()
	rows, err = tx.QueryContext(ctx, query, lookupIDs)
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
