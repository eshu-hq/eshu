// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// StaleActivationSkipObservation captures one registry activation skipped at
// startup because its mounted config file is absent (#9634). Reconciled
// reports whether the startup path also dropped the registry entry. IDs ride
// only on the structured warning log, never on a metric label.
type StaleActivationSkipObservation struct {
	ComponentID string
	InstanceID  string
	Reconciled  bool
}

// newStaleActivationSkipInstruments registers the stale-activation skip
// counter (#9634). It lives outside metrics.go to keep that file under the
// repo's 500-line cap, mirroring retention_metrics.go.
func newStaleActivationSkipInstruments(meter metric.Meter) (metric.Int64Counter, error) {
	staleSkipTotal, err := meter.Int64Counter(
		MetricPrefix+"stale_activation_skips_total",
		metric.WithDescription("Total workflow coordinator startup activations skipped for absent config files (#9634)"),
	)
	if err != nil {
		return staleSkipTotal, fmt.Errorf("register stale activation skips total counter: %w", err)
	}
	return staleSkipTotal, nil
}

func (m *otelMetrics) RecordStaleActivationSkip(ctx context.Context, observation StaleActivationSkipObservation) {
	if m == nil {
		return
	}
	outcome := staleSkipOutcomeError
	if observation.Reconciled {
		outcome = staleSkipOutcomeSuccess
	}
	m.staleSkipTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	))
}
