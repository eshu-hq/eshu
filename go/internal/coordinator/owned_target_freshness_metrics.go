// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// OwnedTargetRotationObservation is the owned-package rotation period one
// derivation read reported for one collector instance (#9775). A Freshness
// that is not [schedule.OwnedTargetFreshness.Known] means the instance has no
// rotation to report now (derivation disabled, single_pass, or no window), and
// the recorder drops the instance's series so a stale breach never lingers.
type OwnedTargetRotationObservation struct {
	CollectorInstanceID string
	Freshness           schedule.OwnedTargetFreshness
}

// ownedTargetRotationGauges serves the owned-target freshness gauges. The last
// observation per collector instance lives behind a mutex because the reconcile
// loop writes it and the metrics reader's callback reads it from another
// goroutine. The map is bounded by the number of vulnerability-intelligence
// instances, so the instance label has bounded cardinality.
type ownedTargetRotationGauges struct {
	mu       sync.Mutex
	byInstID map[string]schedule.OwnedTargetFreshness
}

// newOwnedTargetRotationGauges registers the three #9775 gauges and the one
// callback that observes them. The full literal names are deliberate:
// verify-telemetry-coverage.sh only recognizes a metric whose constructor's
// first argument is a string literal.
func newOwnedTargetRotationGauges(meter metric.Meter) (*ownedTargetRotationGauges, error) {
	gauges := &ownedTargetRotationGauges{byInstID: map[string]schedule.OwnedTargetFreshness{}}
	period, err := meter.Float64ObservableGauge(
		"eshu_dp_owned_target_rotation_period_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Owned-package rotation period in seconds: hash-slot count times the reconcile interval, per collector instance (#9775)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register owned target rotation period gauge: %w", err)
	}
	bound, err := meter.Float64ObservableGauge(
		"eshu_dp_owned_target_max_staleness_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Freshness bound the owned-package rotation period is compared with, per collector instance (#9775)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register owned target max staleness gauge: %w", err)
	}
	exceeds, err := meter.Int64ObservableGauge(
		"eshu_dp_owned_target_rotation_exceeds_max_staleness",
		metric.WithDescription("1 when the owned-package rotation period is longer than its freshness bound, else 0, per collector instance (#9775)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register owned target rotation exceeds gauge: %w", err)
	}
	if _, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		for instanceID, freshness := range gauges.snapshot() {
			attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionCollectorInstanceID, instanceID))
			observer.ObserveFloat64(period, freshness.Period.Seconds(), attrs)
			observer.ObserveFloat64(bound, freshness.MaxStaleness.Seconds(), attrs)
			breach := int64(0)
			if freshness.Exceeds() {
				breach = 1
			}
			observer.ObserveInt64(exceeds, breach, attrs)
		}
		return nil
	}, period, bound, exceeds); err != nil {
		return nil, fmt.Errorf("register owned target rotation callback: %w", err)
	}
	return gauges, nil
}

func (g *ownedTargetRotationGauges) record(observation OwnedTargetRotationObservation) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !observation.Freshness.Known() {
		delete(g.byInstID, observation.CollectorInstanceID)
		return
	}
	g.byInstID[observation.CollectorInstanceID] = observation.Freshness
}

func (g *ownedTargetRotationGauges) snapshot() map[string]schedule.OwnedTargetFreshness {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]schedule.OwnedTargetFreshness, len(g.byInstID))
	for instanceID, freshness := range g.byInstID {
		out[instanceID] = freshness
	}
	return out
}

// RecordOwnedTargetRotation stores the instance's latest rotation period for
// the three freshness gauges (#9775).
func (m *otelMetrics) RecordOwnedTargetRotation(_ context.Context, observation OwnedTargetRotationObservation) {
	if m == nil || m.ownedRotation == nil {
		return
	}
	m.ownedRotation.record(observation)
}
