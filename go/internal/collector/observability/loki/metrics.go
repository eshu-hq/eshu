// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (s *ClaimedSource) recordFetch(ctx context.Context, statusClass string, startedAt time.Time) {
	if s.instruments == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderLoki),
		attribute.String(telemetry.MetricDimensionStatusClass, statusClass),
	)
	s.instruments.LokiProviderRequests.Add(ctx, 1, attrs)
	s.instruments.LokiFetchDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

func (s *ClaimedSource) recordRateLimit(ctx context.Context, failure ProviderFailure) {
	if s.instruments == nil || failure.FailureClass() != FailureRateLimited {
		return
	}
	s.recordRateLimits(ctx, 1)
}

func (s *ClaimedSource) recordRateLimits(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.LokiRateLimited.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderLoki),
	))
}

func (s *ClaimedSource) recordFacts(ctx context.Context, envs []facts.Envelope) {
	if s.instruments == nil {
		return
	}
	counts := map[string]int64{}
	for _, env := range envs {
		counts[env.FactKind]++
	}
	for kind, count := range counts {
		s.instruments.LokiFactsEmitted.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, ProviderLoki),
			attribute.String(telemetry.MetricDimensionFactKind, kind),
		))
	}
}

func (s *ClaimedSource) recordRedactions(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.LokiRedactions.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderLoki),
		attribute.String(telemetry.MetricDimensionReason, "metadata_redacted"),
	))
}

func (s *ClaimedSource) recordRetries(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.LokiRetries.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderLoki),
	))
}

func (s *ClaimedSource) recordStale(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.LokiStale.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderLoki),
	))
}

func (s *ClaimedSource) recordHighCardinality(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.LokiHighCardinalityRejected.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderLoki),
		attribute.String(telemetry.MetricDimensionReason, WarningHighCardinality),
	))
}

func recordObservationStats(freshness string, redacted bool, result *CollectionResult) {
	if freshness == FreshnessStale {
		result.Stats.Stale++
	}
	if redacted {
		result.Stats.Redactions++
	}
}
