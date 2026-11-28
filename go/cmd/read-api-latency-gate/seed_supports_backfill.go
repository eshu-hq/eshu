// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	_ "github.com/jackc/pgx/v5/stdlib"

	pgstore "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// supportsBackfillStandIn is the value the latency report's identity block
// carries in supports_backfill, and the label the seed log line uses. It says
// the supports read model marker this run measures behind was recorded by the
// gate itself, not by a product runtime.
const supportsBackfillStandIn = "gate_stand_in"

// supportsBackfillMaxSteps bounds the stand-in's RunStep loop. The seeded
// corpus holds no finding facts in fact_records (a reject trigger refuses the
// kind), so the first step finds no pair to sweep and moves straight to the
// verification slices, which cover only the seeded supports; a handful of
// steps always suffices. Exceeding the bound means the dirty fence holds rows
// the drain cannot clear, and the seed fails naming that state rather than
// looping until the gate's idle budget burns.
const supportsBackfillMaxSteps = 50

// supportsBackfillStepper is the part of
// pgstore.SupplyChainImpactSupportsBackfill the stand-in needs.
type supportsBackfillStepper interface {
	RunStep(context.Context) (pgstore.SupplyChainImpactSupportsBackfillReport, error)
}

// runSupportsBackfillStandIn runs the product's supports backfill to
// completion and proves it left the read model ready: it loops RunStep until
// the report says Complete, then reads the exact readiness predicate the
// findings serving route refuses behind. It is a permanent seed-phase
// stand-in (#11658): the reducer owns the backfill in production, but no
// reducer runs beside this gate, so a real reducer cannot record the marker
// on the seeded corpus. Seeded findings never enter fact_records, so the
// sweep covers no pairs and the verification proves "no live finding fact
// lacks a support" vacuously over fact_records; genuinely-backed state rests
// on the conjunction this seed already proves elsewhere: the supports were
// written by the production same-transaction maintainer
// (supplychaincore.WriteSupplyChainImpactFindingRows), the count proofs pin
// supports and winners rows, and this stand-in drains the fence, runs the
// product verify slices, and stamps only through its fenced completion
// statement.
//
// It never writes the marker row directly, so a blind stamp over a non-empty
// fence (or over a future seeded finding fact the sweep cannot see) cannot
// pass: after Complete the readiness predicate must read true, and a false
// read fails the seed.
func runSupportsBackfillStandIn(
	ctx context.Context,
	stepper supportsBackfillStepper,
	maxSteps int,
	isReady func(context.Context) (bool, error),
	log io.Writer,
) error {
	_, _ = fmt.Fprintf(log, "read-api-latency-gate: supports_backfill=%s: running the product supports backfill after the seed because no reducer runs beside the gate on the seeded corpus; the stand-in runs the same pgstore.SupplyChainImpactSupportsBackfill the reducer drives and stamps only through its fenced completion statement (#11658); findings route numbers describe an install that holds the marker\n",
		supportsBackfillStandIn)
	var last pgstore.SupplyChainImpactSupportsBackfillReport
	for step := 1; step <= maxSteps; step++ {
		report, err := stepper.RunStep(ctx)
		if err != nil {
			return fmt.Errorf("run supply chain impact supports backfill step %d: %w", step, err)
		}
		last = report
		if report.Complete {
			ready, err := isReady(ctx)
			if err != nil {
				return fmt.Errorf("read supply chain impact supports readiness after the backfill: %w", err)
			}
			if !ready {
				return fmt.Errorf("supply chain impact supports read model not ready after the backfill: the readiness predicate reads false, so the findings route would still refuse; refusing a blind marker stamp")
			}
			_, _ = fmt.Fprintf(log, "read-api-latency-gate: supports read model backfilled (steps=%d written=%d unchanged=%d removed=%d verified=%t)\n",
				step, report.Written, report.Unchanged, report.Removed, report.Verified)
			return nil
		}
	}
	return fmt.Errorf("supply chain impact supports backfill did not complete after %d steps (last DirtyScopes=%d Missing=%d): the fence holds rows the drain cannot clear",
		maxSteps, last.DirtyScopes, last.Missing)
}

// backfillSeededSupportsReadModel wires runSupportsBackfillStandIn to the
// seeded Postgres: the product backfill over a database/sql handle, and the
// serving readiness predicate over the same handle. The handle closes before
// the graph seed so no extra connection lingers through the sweep.
func backfillSeededSupportsReadModel(ctx context.Context, postgresDSN string, log io.Writer) error {
	db, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		return fmt.Errorf("open postgres for the supports read model backfill: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres for the supports read model backfill: %w", err)
	}
	store := pgstore.SQLDB{DB: db}
	return runSupportsBackfillStandIn(ctx,
		pgstore.SupplyChainImpactSupportsBackfill{DB: store},
		supportsBackfillMaxSteps,
		func(ctx context.Context) (bool, error) { return pgstore.SupplyChainImpactSupportsReady(ctx, store) },
		log)
}
