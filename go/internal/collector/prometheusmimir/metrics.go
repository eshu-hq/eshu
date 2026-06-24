// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (s *ClaimedSource) recordFetch(ctx context.Context, target TargetConfig, statusClass string, startedAt time.Time) {
	if s.instruments == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
		attribute.String(telemetry.MetricDimensionStatusClass, statusClass),
	)
	s.instruments.PrometheusMimirProviderRequests.Add(ctx, 1, attrs)
	s.instruments.PrometheusMimirFetchDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

func (s *ClaimedSource) recordRateLimit(ctx context.Context, target TargetConfig, failure ProviderFailure) {
	if s.instruments == nil || failure.FailureClass() != FailureRateLimited {
		return
	}
	s.recordRateLimits(ctx, target, 1)
}

func (s *ClaimedSource) recordRateLimits(ctx context.Context, target TargetConfig, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.PrometheusMimirRateLimited.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}

func (s *ClaimedSource) recordFacts(ctx context.Context, target TargetConfig, envs []facts.Envelope) {
	if s.instruments == nil {
		return
	}
	counts := map[string]int64{}
	for _, env := range envs {
		counts[env.FactKind]++
	}
	for kind, count := range counts {
		s.instruments.PrometheusMimirFactsEmitted.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionFactKind, kind),
		))
	}
}

func (s *ClaimedSource) recordRedactions(ctx context.Context, target TargetConfig, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.PrometheusMimirRedactions.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
		attribute.String(telemetry.MetricDimensionReason, "metadata_redacted"),
	))
}

func (s *ClaimedSource) recordRetries(ctx context.Context, target TargetConfig, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.PrometheusMimirRetries.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}

func (s *ClaimedSource) recordStale(ctx context.Context, target TargetConfig, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.PrometheusMimirStale.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}
