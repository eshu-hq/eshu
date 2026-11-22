// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infra

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Backfiller records the infra read model backfill marker (#6793). Backfill
// blocks until the marker exists, retrying failed attempts itself, and returns
// nil once it does. It returns ctx.Err() when ctx ends first.
type Backfiller interface {
	BackfillInfraInventory(ctx context.Context) error
}

// BackfillRunner starts the infra read model backfill (#11196) so a fresh
// install, a wiped database, or a marker bump reaches the read model without
// an operator step. The backfill's retry, backoff, metric and logs live behind
// the Backfiller port; the runner owns only the process lifetime.
type BackfillRunner struct {
	Backfiller Backfiller
	Logger     *slog.Logger
}

// Run blocks until the backfill records its marker or ctx ends. It returns nil
// on both: the marker is recorded once per install, and a cancelled context is
// a clean shutdown. A failure the Backfiller does not retry itself is returned,
// which stops the reducer: RunBackground only returns the context error.
func (r *BackfillRunner) Run(ctx context.Context) error {
	if r == nil || r.Backfiller == nil {
		return errors.New("infra inventory backfill runner: backfiller is required")
	}
	start := time.Now()
	if r.Logger != nil {
		r.Logger.InfoContext(ctx, "infra inventory backfill runner started",
			"event_name", "infra_inventory.backfill.runner",
			telemetry.PhaseAttr(telemetry.PhaseReduction))
	}
	if err := r.Backfiller.BackfillInfraInventory(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("infra inventory backfill: %w", err)
	}
	if r.Logger != nil {
		r.Logger.InfoContext(ctx, "infra inventory backfill runner finished",
			"event_name", "infra_inventory.backfill.runner",
			"duration_seconds", time.Since(start).Seconds(),
			telemetry.PhaseAttr(telemetry.PhaseReduction))
	}
	return nil
}
