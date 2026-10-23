// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

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
	s.instruments.SecurityAlertProviderRequests.Add(ctx, 1, attrs)
	s.instruments.SecurityAlertFetchDuration.Record(ctx, time.Since(startedAt).Seconds(), attrs)
}

func (s *ClaimedSource) recordRateLimit(ctx context.Context, target TargetConfig, failure ProviderFailure) {
	if s.instruments == nil || failure.FailureClass() != FailureRateLimited {
		return
	}
	s.instruments.SecurityAlertRateLimited.Add(ctx, 1, metric.WithAttributes(
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
		s.instruments.SecurityAlertFactsEmitted.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionFactKind, kind),
		))
	}
}

// Bounded Dependabot endpoint operation tokens used as the operation label of
// eshu_dp_security_alert_truncated_reads_total. They match the tokens the
// provider client stamps on failures (#8258): an endpoint shape, never a
// repository, organization, URL, or credential.
const (
	dependabotRepositoryAlertsOperation   = "list_repo_alerts"
	dependabotOrganizationAlertsOperation = "list_org_alerts"
)

// recordTruncatedRead counts one provider read that hit max_pages before the
// provider stream ended.
func (s *ClaimedSource) recordTruncatedRead(ctx context.Context, target TargetConfig, operation string) {
	if s.instruments == nil {
		return
	}
	s.instruments.SecurityAlertTruncatedReads.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
		attribute.String(telemetry.MetricDimensionOperation, operation),
	))
}

// recordFanoutOutcome counts one allowlisted repository handled by the org
// fan-out, labeled by its closed outcome.
func (s *ClaimedSource) recordFanoutOutcome(ctx context.Context, target TargetConfig, outcome string) {
	if s.instruments == nil {
		return
	}
	s.instruments.SecurityAlertOrgFanoutRepositories.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	))
}
