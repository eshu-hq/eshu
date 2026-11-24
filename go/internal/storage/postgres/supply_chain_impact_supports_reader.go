// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Reducer-side readers of the per-support table (#10880 PR-B2). Every reader
// that reads supply-chain impact findings as reducer input from the supports
// table, instead of the finding facts, checks readiness through the one helper
// below and refuses with the one typed refusal
// (supplychaincore.SupplyChainImpactSupportsNotReadyError, a non-counting
// retryable class) while the table is not ready. A support row carries the fact
// payload verbatim (migration 338), so a reader that decodes the payload sees
// the same fields the fact read saw.

// requireSupplyChainImpactSupportsReady returns nil only when the supports table
// is ready: the backfill marker is complete at this binary's contract version,
// no marker for a higher version exists, and the old-writer fence is empty
// (supplyChainImpactSupportsReadySQL, the pinned readiness predicate). A
// not-ready table returns the typed non-counting refusal; a readiness probe that
// fails returns its own error under its own classification, never the not-ready
// class. The outcome lands on eshu_dp_supply_chain_impact_supports_reads_total
// (reader, outcome=not_ready or failed); a read that ran is counted by the
// caller as served after it returns. A nil instruments records nothing.
func requireSupplyChainImpactSupportsReady(
	ctx context.Context,
	queryer db.Queryer,
	instruments *telemetry.Instruments,
	reader string,
) error {
	ready, err := SupplyChainImpactSupportsReady(ctx, queryer)
	if err != nil {
		instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadFailed)
		return err
	}
	if !ready {
		instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadNotReady)
		return &supplychaincore.SupplyChainImpactSupportsNotReadyError{Reader: reader}
	}
	return nil
}

// RequireSupplyChainImpactSupportsReady is the exported form of the helper for
// readers outside this package that hold only a queryer (the coordinator's
// Go-modules fence): the same predicate, the same typed refusal, the same
// counter.
func RequireSupplyChainImpactSupportsReady(
	ctx context.Context,
	queryer db.Queryer,
	instruments *telemetry.Instruments,
	reader string,
) error {
	return requireSupplyChainImpactSupportsReady(ctx, queryer, instruments, reader)
}
