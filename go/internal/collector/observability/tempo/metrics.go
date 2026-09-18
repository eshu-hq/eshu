// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

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
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
		attribute.String(telemetry.MetricDimensionStatusClass, statusClass),
	)
	s.instruments.TempoProviderRequests.Add(ctx, 1, attrs)
	s.instruments.TempoFetchDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

func (s *ClaimedSource) recordRateLimit(ctx context.Context, failure ProviderFailure) {
	if s.instruments == nil || failure.FailureClass() != FailureRateLimited {
		return
	}
	s.instruments.TempoRateLimited.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
	))
}

func (s *ClaimedSource) recordRateLimits(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.TempoRateLimited.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
	))
}

func (s *ClaimedSource) recordRetries(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.TempoRetries.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
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
		s.instruments.TempoFactsEmitted.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
			attribute.String(telemetry.MetricDimensionFactKind, kind),
		))
	}
}

func (s *ClaimedSource) recordRedactions(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.TempoRedactions.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
	))
}

func (s *ClaimedSource) recordStale(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.TempoStale.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
	))
}

func (s *ClaimedSource) recordHighCardinality(ctx context.Context, count int) {
	if s.instruments == nil || count <= 0 {
		return
	}
	s.instruments.TempoHighCardinalityRejected.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, ProviderTempo),
	))
}
