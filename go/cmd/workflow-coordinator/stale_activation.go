// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/coordinator"
)

// reportStaleActivationSkips reconciles registry activations skipped at
// startup for absent config files, then reports each on the process logger
// and coordinator metrics (#9634). Every signal stays low-cardinality: the
// warning log carries the component and instance IDs, the counter carries
// only the reconcile outcome. Reconcile and reporting are best-effort and
// never fail startup: the skip already protects planning, so a failed drop
// is retried on the next restart.
func reportStaleActivationSkips(ctx context.Context, metrics coordinator.Metrics, logger *slog.Logger, home string, skips []coordinator.StaleActivationSkip) {
	if len(skips) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	outcomes := coordinator.ReconcileStaleComponentActivations(home, skips)
	for i, skip := range skips {
		reconciled := false
		var reconcileErr error
		if i < len(outcomes) {
			reconciled = outcomes[i].Dropped
			reconcileErr = outcomes[i].Err
		}
		if logger != nil {
			args := []any{
				"component_id", strings.TrimSpace(skip.ComponentID),
				"instance_id", strings.TrimSpace(skip.InstanceID),
				"reconciled", reconciled,
			}
			if reconcileErr != nil {
				args = append(args, "error", reconcileErr.Error())
			}
			logger.WarnContext(ctx, "workflow coordinator skipped stale component activation with missing config file", args...)
		}
		if metrics != nil {
			metrics.RecordStaleActivationSkip(ctx, coordinator.StaleActivationSkipObservation{
				ComponentID: skip.ComponentID,
				InstanceID:  skip.InstanceID,
				Reconciled:  reconciled,
			})
		}
	}
}
