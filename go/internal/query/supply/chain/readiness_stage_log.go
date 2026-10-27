// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// doneReadinessStage closes a readiness_snapshot stage. A failed read answers
// readiness_unavailable without failing the route, so the completion line
// rises to WARN and names the closed-set readiness_unavailable_cause plus the
// bounded error text. A malformed, duplicate, or overflowing source-target
// configuration blocks every readiness read until it is fixed, and this line
// is how an operator tells that from a transient query error.
func doneReadinessStage(ctx context.Context, timer supplyChainQueryStageTimer, err error, evidenceSourceCount int) {
	attrs := []slog.Attr{
		slog.Bool("error", err != nil),
		slog.Int("evidence_source_count", evidenceSourceCount),
	}
	if err == nil {
		timer.Done(ctx, attrs...)
		return
	}
	attrs = append(attrs,
		slog.String("readiness_unavailable_cause", impact.ReadinessUnavailableCause(err)),
		slog.String("readiness_error", boundedErrorText(err, stageFailedErrorMaxBytes)),
	)
	timer.DoneAt(ctx, slog.LevelWarn, attrs...)
}
