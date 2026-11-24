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
	return observeSupplyChainImpactSupportsReadiness(ctx, instruments, reader, ready, err)
}

// observeSupplyChainImpactSupportsReadiness classifies one readiness outcome: a
// probe error is recorded as failed and returned under its own class, a table that
// is not ready is recorded as not_ready and returns the typed non-counting refusal,
// and a ready table records nothing. It is the one classification both the
// stand-alone readers and the Go-modules fence's one-statement read share.
func observeSupplyChainImpactSupportsReadiness(
	ctx context.Context,
	instruments *telemetry.Instruments,
	reader string,
	ready bool,
	err error,
) error {
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

// SupplyChainImpactSupportsReadyBinding is the readiness predicate as plain values
// for a reader whose package cannot import this one (the Go-modules fence, whose
// holder statement embeds the predicate so both are read in one snapshot, #11197).
// SQL is supplyChainImpactSupportsReadySQL itself, Args are its parameters in order
// (the backfill marker name, then the contract version), and Observe is the shared
// classification above under the reader's label. The fence's SupportsReadiness has
// the same fields; the composition root converts one to the other.
type SupplyChainImpactSupportsReadyBinding struct {
	SQL     string
	Args    []any
	Observe func(ctx context.Context, ready bool, err error) error
}

// NewSupplyChainImpactSupportsReadyBinding returns the binding for one reader. A nil
// instruments records nothing.
func NewSupplyChainImpactSupportsReadyBinding(
	instruments *telemetry.Instruments,
	reader string,
) SupplyChainImpactSupportsReadyBinding {
	return SupplyChainImpactSupportsReadyBinding{
		SQL:  supplyChainImpactSupportsReadySQL,
		Args: []any{SupplyChainImpactSupportsBackfillMarker, SupplyChainImpactSupportsContractVersion},
		Observe: func(ctx context.Context, ready bool, err error) error {
			return observeSupplyChainImpactSupportsReadiness(ctx, instruments, reader, ready, err)
		},
	}
}

// RequireSupplyChainImpactSupportsReady is the exported form of the helper for
// callers outside this package that already hold a repeatable-read snapshot and
// only a queryer: the same predicate, the same typed refusal, the same counter.
// A READ COMMITTED reader must not call it before a separate read, because the
// check and the read would be two snapshots: it embeds the predicate in its own
// statement through NewSupplyChainImpactSupportsReadyBinding, as the coordinator's
// Go-modules fence does.
func RequireSupplyChainImpactSupportsReady(
	ctx context.Context,
	queryer db.Queryer,
	instruments *telemetry.Instruments,
	reader string,
) error {
	return requireSupplyChainImpactSupportsReady(ctx, queryer, instruments, reader)
}
