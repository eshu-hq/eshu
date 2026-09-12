// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// resourceListMetrics holds the duration histogram and error counter for the
// bounded IaC resource list handler. The query package records through the
// global OTEL meter (mirroring how startQueryHandlerSpan uses the global
// tracer); the canonical names, descriptions, and buckets are frozen in
// go/internal/telemetry/instruments.go. Obtaining the instruments here by the
// same names writes to the same SDK provider, so dashboards see a single
// series.
type resourceListMetrics struct {
	duration metric.Float64Histogram
	errors   metric.Int64Counter
}

var (
	resourceListMetricsOnce sync.Once
	resourceListMetricsInst resourceListMetrics
)

// resourceMetrics lazily builds the IaC resource list instruments from the
// global meter. Float64Histogram and Int64Counter never fail with the default
// global meter, so registration errors leave the instruments nil and the
// caller's nil-guarded record calls become no-ops rather than panics.
func resourceMetrics() resourceListMetrics {
	resourceListMetricsOnce.Do(func() {
		meter := otel.Meter("eshu/go/internal/query")
		buckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
		if h, err := meter.Float64Histogram(
			"eshu_dp_iac_resource_list_duration_seconds",
			metric.WithDescription("Bounded IaC resource list (GET /api/v0/iac/resources) handler duration"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(buckets...),
		); err == nil {
			resourceListMetricsInst.duration = h
		}
		if c, err := meter.Int64Counter(
			"eshu_dp_iac_resource_list_errors_total",
			metric.WithDescription("Bounded IaC resource list (GET /api/v0/iac/resources) handler errors"),
		); err == nil {
			resourceListMetricsInst.errors = c
		}
	})
	return resourceListMetricsInst
}

// recordDuration reports handler latency in seconds, tagged with the resolved
// IaC kind so operators can compare resource, module, and data-source reads.
func (m resourceListMetrics) recordDuration(ctx context.Context, kind string, seconds float64) {
	if m.duration == nil {
		return
	}
	m.duration.Record(ctx, seconds, metric.WithAttributes(attribute.String("iac.kind", kind)))
}

// recordError counts one handler failure, tagged with a bounded reason label so
// alerting can separate validation rejections from graph-read failures.
func (m resourceListMetrics) recordError(ctx context.Context, kind, reason string) {
	if m.errors == nil {
		return
	}
	m.errors.Add(ctx, 1, metric.WithAttributes(
		attribute.String("iac.kind", kind),
		attribute.String("reason", reason),
	))
}
