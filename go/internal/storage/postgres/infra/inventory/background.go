// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultBackfillInitialBackoff = 30 * time.Second
	defaultBackfillMaxBackoff     = 10 * time.Minute
)

// BackgroundOptions configures RunBackground. Zero backoffs use the defaults
// (30s doubling up to 10m).
type BackgroundOptions struct {
	Logger         *slog.Logger
	Instruments    *telemetry.Instruments
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// RunBackground runs the backfill until its marker is recorded or ctx ends.
// A failed attempt (for example migration 109 not applied yet, or a transient
// Postgres error) is logged, counted, and retried after a doubling backoff,
// so readers do not stay on the graph path until the process restarts. It
// returns nil once the marker exists and ctx.Err() if ctx ends first.
//
// Every attempt is counted in eshu_dp_infra_inventory_backfill_runs_total by
// outcome: completed, already_complete, or failed.
func RunBackground(ctx context.Context, database db.ExecQueryer, opts BackgroundOptions) error {
	backoff := opts.InitialBackoff
	if backoff <= 0 {
		backoff = defaultBackfillInitialBackoff
	}
	maxBackoff := opts.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultBackfillMaxBackoff
	}
	backfiller := Backfiller{DB: database, Logger: opts.Logger}
	for {
		result, err := backfiller.Run(ctx)
		switch {
		case err == nil && result.AlreadyComplete:
			recordBackfillRun(ctx, opts.Instruments, "already_complete")
			return nil
		case err == nil:
			recordBackfillRun(ctx, opts.Instruments, "completed")
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		recordBackfillRun(ctx, opts.Instruments, "failed")
		if opts.Logger != nil {
			opts.Logger.ErrorContext(ctx, "infra inventory backfill failed; aggregate reads stay on the graph until it completes",
				"event_name", "infra_inventory.backfill.failed", "error", err,
				"read_model_installed", !IsNotInstalled(err), "retry_in_seconds", backoff.Seconds())
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func recordBackfillRun(ctx context.Context, instruments *telemetry.Instruments, outcome string) {
	if instruments == nil || instruments.InfraInventoryBackfillRuns == nil {
		return
	}
	instruments.InfraInventoryBackfillRuns.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome)))
}
