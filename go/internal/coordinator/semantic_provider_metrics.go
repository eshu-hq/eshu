// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SemanticProviderClaimObservation captures one semantic-provider worker claim
// outcome with only redacted, low-cardinality dimensions. It never carries a
// provider host, endpoint, URL, credential, raw prompt, or raw response.
type SemanticProviderClaimObservation struct {
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

// SemanticProviderWorkerMetrics records semantic-provider worker claim telemetry.
type SemanticProviderWorkerMetrics interface {
	RecordSemanticProviderClaim(context.Context, SemanticProviderClaimObservation)
}

const (
	semanticProviderMetricDimensionProviderKind = "provider_kind"
	semanticProviderMetricDimensionProfileClass = "provider_profile_class"
)

type otelSemanticProviderWorkerMetrics struct {
	claimTotal metric.Int64Counter
}

// NewSemanticProviderWorkerMetrics registers semantic-provider worker instruments.
func NewSemanticProviderWorkerMetrics(meter metric.Meter) (SemanticProviderWorkerMetrics, error) {
	if meter == nil {
		return nil, fmt.Errorf("meter is required")
	}
	claimTotal, err := meter.Int64Counter(
		coordinatorMetricPrefix+"semantic_provider_claim_total",
		metric.WithDescription("Total semantic-provider worker claim outcomes by egress decision and terminal disposition"),
	)
	if err != nil {
		return nil, fmt.Errorf("register semantic provider claim counter: %w", err)
	}
	return &otelSemanticProviderWorkerMetrics{claimTotal: claimTotal}, nil
}

func (m *otelSemanticProviderWorkerMetrics) RecordSemanticProviderClaim(
	ctx context.Context,
	observation SemanticProviderClaimObservation,
) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, redactedLabel(observation.Outcome)),
		attribute.String(semanticProviderMetricDimensionProviderKind, redactedLabel(observation.ProviderKind)),
		attribute.String(semanticProviderMetricDimensionProfileClass, redactedLabel(observation.ProviderProfileClass)),
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
