// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// recordIncidentContextStage marks the end of one bounded incident read stage.
// It records only a fixed stage name, duration, and error flag; request data
// stays out of the trace attributes.
func recordIncidentContextStage(ctx context.Context, stage string, started time.Time, err error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.AddEvent("query.incident_context.stage", trace.WithAttributes(
		attribute.String("eshu.incident_context.stage", stage),
		attribute.Float64("eshu.incident_context.duration_ms", time.Since(started).Seconds()*1000),
		attribute.Bool("eshu.incident_context.error", err != nil),
	))
}
