// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ScrapeObservation is what one runtime /metrics scrape decided about the
// active-work summary.
type ScrapeObservation struct {
	// ModelKey is the model the scrape selected; the caller sets it.
	ModelKey string
	// Source is SourceModel, SourceLastRow, or SourceZero.
	Source Source
	// Reason explains Source.
	Reason Reason
	// AsOf is the as_of of the row served; zero for SourceZero.
	AsOf time.Time
	// Age is the served row's age at the scrape; zero for SourceZero.
	Age time.Duration
}

// scrapeLimiter spaces the per-process stale-scrape Warn by reason. It is
// separate from fallbackLimiter so a status route's fallback Warn and a
// scrape's stale Warn never suppress each other.
var scrapeLimiter warnLimiter

// ObserveScrape records one scrape decision: the scrape counter, the source,
// age, and reason on the current span, and a rate-limited Warn when the scrape
// served a row other than a fresh one, so the writer's health is visible from
// the pods that cannot reach it. instruments may be nil. A fresh serve never
// logs.
func ObserveScrape(ctx context.Context, instruments *telemetry.Instruments, o ScrapeObservation) {
	source, reason := string(o.Source), string(o.Reason)
	if instruments != nil && instruments.StatusSummaryScrapes != nil {
		instruments.StatusSummaryScrapes.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrModelKey(o.ModelKey), telemetry.AttrSummarySource(source), telemetry.AttrSummaryReason(reason),
		))
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String(spanAttrSource, source))
	if !o.AsOf.IsZero() {
		span.SetAttributes(attribute.Float64(spanAttrAgeSeconds, o.Age.Seconds()))
	}
	if o.Source == SourceModel {
		return
	}
	span.SetAttributes(attribute.String(spanAttrFallbackWhy, reason))
	if scrapeLimiter.allow(o.Reason, time.Now()) {
		slog.Default().WarnContext(ctx, "status summary row not served fresh on the metrics scrape; serving the last decoded row or the zero summary",
			slog.String("model_key", o.ModelKey),
			slog.String("source", source),
			slog.String("reason", reason),
			slog.Float64("age_seconds", o.Age.Seconds()),
			telemetry.FailureClassAttr("status_summary_scrape_stale"),
		)
	}
}
