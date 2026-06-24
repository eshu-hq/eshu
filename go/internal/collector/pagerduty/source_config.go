// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (s *ClaimedSource) collectConfigEvidence(
	ctx context.Context,
	target targetRuntime,
) (ConfigCollectionResult, error) {
	if !target.config.ConfigValidationEnabled {
		return ConfigCollectionResult{}, nil
	}
	client, ok := target.client.(ConfigEvidenceClient)
	if !ok {
		return ConfigCollectionResult{
			Warnings: []ConfigWarning{{
				ResourceClass: "collector",
				Reason:        ConfigWarningUnsupported,
			}},
			ObservedAt: s.now().UTC(),
			Partial:    true,
		}, nil
	}
	result, err := client.CollectConfigEvidence(ctx, target.config)
	if err != nil {
		return ConfigCollectionResult{}, err
	}
	if result.ObservedAt.IsZero() {
		result.ObservedAt = s.now().UTC()
	}
	return result, nil
}

func collectionObservedAt(
	incidentResult CollectionResult,
	configResult ConfigCollectionResult,
	now time.Time,
) time.Time {
	if !incidentResult.ObservedAt.IsZero() {
		return incidentResult.ObservedAt.UTC()
	}
	if !configResult.ObservedAt.IsZero() {
		return configResult.ObservedAt.UTC()
	}
	return now.UTC()
}

func (s *ClaimedSource) recordConfigTelemetry(
	ctx context.Context,
	target TargetConfig,
	result ConfigCollectionResult,
) {
	if s.instruments == nil || !target.ConfigValidationEnabled {
		return
	}
	s.recordObservedConfigResources(ctx, target, result)
	s.recordConfigWarnings(ctx, target, result)
	if result.Redactions > 0 {
		s.instruments.PagerDutyConfigRedactions.Add(ctx, int64(result.Redactions), metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionReason, "sensitive_field"),
		))
	}
}

func (s *ClaimedSource) recordObservedConfigResources(
	ctx context.Context,
	target TargetConfig,
	result ConfigCollectionResult,
) {
	resourceCounts := map[string]int64{}
	driftCounts := map[string]int64{}
	for _, service := range result.Services {
		resourceCounts[ConfigResourceClassService]++
		if reason := strings.TrimSpace(service.DriftReason); reason != "" {
			driftCounts[reason]++
		}
	}
	for _, integration := range result.Integrations {
		resourceCounts[ConfigResourceClassServiceIntegration]++
		if reason := strings.TrimSpace(integration.DriftReason); reason != "" {
			driftCounts[reason]++
		}
	}
	for resourceType, count := range resourceCounts {
		s.instruments.PagerDutyConfigResourcesObserved.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionResourceType, resourceType),
		))
	}
	for reason, count := range driftCounts {
		s.instruments.PagerDutyConfigDriftCandidates.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionReason, reason),
		))
	}
}

func (s *ClaimedSource) recordConfigWarnings(
	ctx context.Context,
	target TargetConfig,
	result ConfigCollectionResult,
) {
	warningCounts := map[string]int64{}
	for _, warning := range result.Warnings {
		reason := strings.TrimSpace(warning.Reason)
		if reason == "" {
			reason = ConfigWarningPartial
		}
		warningCounts[reason]++
	}
	if result.Partial && len(result.Warnings) == 0 {
		warningCounts[ConfigWarningPartial]++
	}
	for reason, count := range warningCounts {
		s.instruments.PagerDutyConfigPartialFailures.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, target.Provider),
			attribute.String(telemetry.MetricDimensionReason, reason),
		))
	}
}
