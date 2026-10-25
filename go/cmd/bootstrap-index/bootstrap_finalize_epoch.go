// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	secretlines "github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reconcileUnclaimedFinalize decides what a bootstrap Finalize that claimed
// nothing means (#8833 ruling-4). The state row's epoch tells the cases apart:
//
//   - A newer epoch than this run's: a newer bulk load or a migration owns the
//     table now (the reducer's Repairer rebuilds it). This run's rows are still
//     in place, so it logs a Warn and exits cleanly.
//   - The run's own epoch: someone finalized it, or the claim failed, while this
//     run still held the lock. The run lock and the deferred-writer lock make
//     that unreachable, so it is a tripwire for lost exclusivity: ERROR and a
//     non-nil error, never a silent success.
func reconcileUnclaimedFinalize(ctx context.Context, queryer db.Queryer, epoch int64, logger *slog.Logger) error {
	row, err := secretlines.ReadState(ctx, queryer)
	if err != nil {
		return logLostEpoch(ctx, logger, epoch, fmt.Errorf("finalize claimed nothing and the state read failed: %w", err))
	}
	if !row.Found {
		return logLostEpoch(ctx, logger, epoch, errors.New("finalize claimed nothing and there is no state row"))
	}
	if row.Epoch > epoch {
		logger.WarnContext(ctx, "secret lines finalization superseded by a newer epoch; the reducer owns the rebuild",
			"event_name", "secret_lines.finalize_superseded", "epoch", epoch, "current_epoch", row.Epoch, "state", row.State)
		return nil
	}
	return logLostEpoch(ctx, logger, epoch, fmt.Errorf(
		"secret lines finalize claimed nothing for epoch %d (state %s, epoch %d): lost exclusivity over the bulk load",
		epoch, row.State, row.Epoch))
}

func logLostEpoch(ctx context.Context, logger *slog.Logger, epoch int64, err error) error {
	logger.ErrorContext(ctx, "secret lines finalize did not claim this run's epoch",
		"event_name", "secret_lines.finalize_lost_epoch", "epoch", epoch,
		telemetry.FailureClassAttr("secret_lines_bulk_load_lock_lost"), "error", err)
	return err
}
