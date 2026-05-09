package terraformstate

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type otelDiscoveryMetrics struct {
	candidates metric.Int64Counter
}

// NewDiscoveryMetrics registers Terraform-state discovery metrics.
func NewDiscoveryMetrics(meter metric.Meter) (DiscoveryMetrics, error) {
	if meter == nil {
		return nil, errors.New("meter is required")
	}
	candidates, err := meter.Int64Counter(
		"eshu_dp_tfstate_discovery_candidates_total",
		metric.WithDescription("Total Terraform state discovery candidates resolved by source"),
	)
	if err != nil {
		return nil, err
	}
	return otelDiscoveryMetrics{candidates: candidates}, nil
}

// RecordCandidates records resolved candidate counts by discovery source.
func (m otelDiscoveryMetrics) RecordCandidates(ctx context.Context, source DiscoveryCandidateSource, count int) {
	if count <= 0 {
		return
	}
	m.candidates.Add(ctx, int64(count), metric.WithAttributes(attribute.String("source", string(source))))
}
