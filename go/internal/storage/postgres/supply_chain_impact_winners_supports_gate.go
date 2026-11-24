// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// selectSupplyChainImpactWinnersSupportsGateSQL reads the winners maintainer's
// supports gate in one statement and so one snapshot (#10880 PR-C1 R2). ready is
// the pinned supports readiness predicate, embedded unchanged, so the maintainer
// is gated by exactly what gates the writer and the readers. full_needed is true
// when the supports marker completed after the winners watermark was last
// committed: the winners were last written before the table they read was
// complete, so one full must run. A watermark that was never written reads as
// -infinity (the contract decision runs the full for it already); a marker that
// has not completed reads as -infinity too, so it never asks for a full.
// Arguments: $1 backfill marker name, $2 supports contract version.
const selectSupplyChainImpactWinnersSupportsGateSQL = `
SELECT gate.ready,
       gate.ready AND COALESCE(
           (SELECT completed_at FROM supply_chain_impact_finding_supports_backfill WHERE marker_name = $1),
           '-infinity'::timestamptz)
         > COALESCE(
           (SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1),
           '-infinity'::timestamptz) AS full_needed
FROM (` + supplyChainImpactSupportsReadySQL + `) AS gate(ready)
`

// WinnersSupportsGate reports whether the supports table the maintainer's inputs
// read is ready, and whether the first ready tick after a not-ready period still
// owes a full. Implemented for the winners maintainer's SupportsGate.
func (s SupplyChainImpactWinnersStore) WinnersSupportsGate(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersSupportsGate, error) {
	if s.DB == nil {
		return supplychaincore.SupplyChainImpactWinnersSupportsGate{}, fmt.Errorf("supply chain impact winners database is required")
	}
	return readWinnersSupportsGate(ctx, s.DB)
}

// readWinnersSupportsGate runs the gate statement through q. An empty answer is an
// error: a gate that read nothing must not be taken for a ready table.
func readWinnersSupportsGate(ctx context.Context, q db.Queryer) (supplychaincore.SupplyChainImpactWinnersSupportsGate, error) {
	var gate supplychaincore.SupplyChainImpactWinnersSupportsGate
	rows, err := q.QueryContext(ctx, selectSupplyChainImpactWinnersSupportsGateSQL,
		SupplyChainImpactSupportsBackfillMarker, SupplyChainImpactSupportsContractVersion)
	if err != nil {
		return gate, fmt.Errorf("read supply chain impact winners supports gate: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return gate, fmt.Errorf("read supply chain impact winners supports gate: %w", err)
		}
		return gate, fmt.Errorf("read supply chain impact winners supports gate: no row")
	}
	if err := rows.Scan(&gate.Ready, &gate.FullNeeded); err != nil {
		return gate, fmt.Errorf("scan supply chain impact winners supports gate: %w", err)
	}
	return gate, rows.Err()
}

var _ supplychaincore.SupplyChainImpactWinnersSupportsGateReader = SupplyChainImpactWinnersStore{}
