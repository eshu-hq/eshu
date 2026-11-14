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

// holderMassProbeQuery estimates, per lookup id, how many live finding rows
// name that id, so the reducer can isolate hot ids into their own holder-read
// statement (#10643). Each arm joins one id at a time against the same
// partial index the shipped holder read uses (literal fact kind plus the
// tombstone predicate prove the predicate), so the probe avoids the scope
// join and DISTINCT sort of a second full read; at production mass it
// measured 6067 ms cold / 218 ms warm (see the evidence note), bounded by
// the same guard with fail-closed errors.
// The probe
// deliberately overestimates: it counts rows in superseded generations (no
// active-generation join) and counts a row once per arm it matches, and an
// id whose row matches through two ids in one chunk counts toward both. All
// three biases point at splitting, which is the safe direction — an extra
// statement stays far under the guard, while a missing split could re-form
// the timed-out shape. The outer LEFT JOIN reports every requested id with
// zero for no match, so the planner only treats an id as unmeasured when the
// probe itself misbehaves. It runs under the same statement timeout as the
// holder read through readIDMasses.
const holderMassProbeQuery = `
SELECT ids.id AS lookup_id, COUNT(candidates.lookup_id) AS mass
FROM UNNEST($1::text[]) AS ids(id)
LEFT JOIN (
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
    UNION ALL
    SELECT per_arm.id AS lookup_id
    FROM UNNEST($1::text[]) AS per_arm(id)
    JOIN fact_records AS finding
      ON finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
     AND finding.is_tombstone = FALSE
     AND finding.payload #> '{provenance,advisory_group_members}' ? per_arm.id
) AS candidates ON candidates.lookup_id = ids.id
GROUP BY ids.id
`

// EstimateHolderMasses implements supplychaincore.HolderMassEstimator: one row
// per requested lookup id with its live finding-row count, under the same
// bounded statement timeout as the holder read. A timeout classifies as
// supplychaincore.ErrFanOutReadTimeout exactly like the holder read, so the
// pass fails (and retries) instead of splitting blind.
func (s SupplyChainImpactFanOutStore) EstimateHolderMasses(
	ctx context.Context,
	lookupIDs []string,
) (map[string]int64, error) {
	if len(lookupIDs) == 0 {
		return map[string]int64{}, nil
	}
	return s.readIDMasses(ctx, holderMassProbeQuery, lookupIDs)
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
