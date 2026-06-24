// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

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
	s.instruments.JiraProviderRequests.Add(ctx, 1, attrs)
	s.instruments.JiraFetchDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

func (s *ClaimedSource) recordRateLimit(ctx context.Context, target TargetConfig, failure ProviderFailure) {
	if s.instruments == nil || failure.FailureClass() != FailureRateLimited {
		return
	}
	s.instruments.JiraRateLimited.Add(ctx, 1, metric.WithAttributes(
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
		s.instruments.JiraFactsEmitted.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionFactKind, kind),
		))
	}
}
