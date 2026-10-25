// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordRead records one graph read's duration under its closed operation and
// outcome. It tolerates a nil handler and nil instruments so the handler stays
// operable when telemetry is not wired (tests, the lightweight profile).
func (h *Handler) recordRead(ctx context.Context, operation string, startedAt time.Time, err error) {
	recordRead(ctx, h.instruments(), operation, startedAt, err)
}

// recordRead is the shared recorder; the repository context section calls it
// through RecordSectionRead.
func recordRead(ctx context.Context, inst *telemetry.Instruments, operation string, startedAt time.Time, err error) {
	if inst == nil || inst.RuntimeDeclarationQueryDuration == nil {
		return
	}
	outcome := telemetry.RuntimeDeclarationOutcomeOK
	if err != nil {
		outcome = telemetry.RuntimeDeclarationOutcomeError
	}
	inst.RuntimeDeclarationQueryDuration.Record(
		ctx,
		time.Since(startedAt).Seconds(),
		metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("outcome", outcome),
		),
	)
}

// recordExcluded counts the declarations a comparison could not place, by
// closed class.
func (h *Handler) recordExcluded(ctx context.Context, summary excludedSummary) {
	inst := h.instruments()
	if inst == nil || inst.RuntimeDeclarationQueryExcluded == nil {
		return
	}
	for class, count := range summary.BySpecKind {
		inst.RuntimeDeclarationQueryExcluded.Add(
			ctx,
			int64(count),
			metric.WithAttributes(attribute.String("reason", class)),
		)
	}
}

func (h *Handler) instruments() *telemetry.Instruments {
	if h == nil {
		return nil
	}
	return h.Instruments
}
