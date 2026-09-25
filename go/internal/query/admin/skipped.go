// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// skippedScopesResponse renders the refinalize skipped-scope report for the
// recover-generations and refinalize responses (#7116). It always carries all
// three keys, with empty objects when nothing was skipped, so "nothing skipped"
// is distinguishable from a server that predates the report.
func skippedScopesResponse(skipped recovery.SkippedScopes) recovery.SkippedScopesReport {
	return skipped.Report()
}

// reportRefinalizeOutcome emits the operator signals for one completed
// refinalize: a structured log line carrying the enqueued and skipped counts,
// and one counter increment per skip reason. It is what lets an operator with
// only logs and dashboards see that a rebuild was partial.
//
// The log is at Info when nothing was skipped and Warn when anything was, so a
// partial rebuild stands out in a log stream without a query on the fields.
// Scope ids stay out of the log and metric labels; the response body carries a
// bounded sample for the caller.
func (h *Handler) reportRefinalizeOutcome(ctx context.Context, operation string, mode string, result recovery.RefinalizeResult) {
	attrs := []any{
		"operation", operation,
		"mode", mode,
		"enqueued", result.Enqueued,
		"generations_retired", result.GenerationsRetired,
		"skipped_total", result.Skipped.Total(),
	}
	for _, reason := range result.Skipped.Reasons() {
		attrs = append(attrs, "skipped_"+reason, result.Skipped.ByReason[reason])
	}

	level := slog.LevelInfo
	if result.Skipped.Total() > 0 {
		level = slog.LevelWarn
	}
	slog.Log(ctx, level, operation+" completed", attrs...)

	if h.Instruments == nil || h.Instruments.RecoveryScopesSkipped == nil {
		return
	}
	for _, reason := range result.Skipped.Reasons() {
		h.Instruments.RecoveryScopesSkipped.Add(
			ctx, int64(result.Skipped.ByReason[reason]),
			metric.WithAttributes(telemetry.AttrReason(reason)),
		)
	}
}
