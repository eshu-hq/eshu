// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ProviderClaimObservation captures one semantic-provider worker claim
// outcome with only redacted, low-cardinality dimensions. It never carries a
// provider host, endpoint, URL, credential, raw prompt, or raw response.
type ProviderClaimObservation struct {
	// Outcome is a bounded outcome code (egress_denied, egress_policy_missing,
	// provider_disabled, dispatched, provider_unavailable).
	Outcome string
	// ProviderKind is the low-cardinality provider kind (e.g. a class label).
	ProviderKind string
	// ProviderProfileClass is the low-cardinality profile class label.
	ProviderProfileClass string
	// SourceClass is the bounded source class label.
	SourceClass string
}

// ProviderWorkerMetrics records semantic-provider worker claim telemetry.
type ProviderWorkerMetrics interface {
	RecordSemanticProviderClaim(context.Context, ProviderClaimObservation)
}

const (
	providerMetricDimensionProviderKind = "provider_kind"
	providerMetricDimensionProfileClass = "provider_profile_class"
)

type otelProviderWorkerMetrics struct {
	claimTotal metric.Int64Counter
}

// NewProviderWorkerMetrics registers semantic-provider worker instruments
// under metricPrefix, the coordinator-wide instrument namespace
// ([github.com/eshu-hq/eshu/go/internal/coordinator.MetricPrefix]). It is
// passed in rather than imported so this package stays free of a dependency
// on the coordinator root.
func NewProviderWorkerMetrics(meter metric.Meter, metricPrefix string) (ProviderWorkerMetrics, error) {
	if meter == nil {
		return nil, fmt.Errorf("meter is required")
	}
	claimTotal, err := meter.Int64Counter(
		metricPrefix+"semantic_provider_claim_total",
		metric.WithDescription("Total semantic-provider worker claim outcomes by egress decision and terminal disposition"),
	)
	if err != nil {
		return nil, fmt.Errorf("register semantic provider claim counter: %w", err)
	}
	return &otelProviderWorkerMetrics{claimTotal: claimTotal}, nil
}

func (m *otelProviderWorkerMetrics) RecordSemanticProviderClaim(
	ctx context.Context,
	observation ProviderClaimObservation,
) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, redactedLabel(observation.Outcome)),
		attribute.String(providerMetricDimensionProviderKind, redactedLabel(observation.ProviderKind)),
		attribute.String(providerMetricDimensionProfileClass, redactedLabel(observation.ProviderProfileClass)),
		attribute.String(telemetry.MetricDimensionSourceClass, redactedLabel(observation.SourceClass)),
	)
	m.claimTotal.Add(ctx, 1, attrs)
}

// redactedLabel keeps metric cardinality bounded and avoids empty-string labels.
func redactedLabel(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
