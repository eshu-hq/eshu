// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// cloudResourceListMeterName scopes the lazily registered cloud-resource list
// instruments to this package. It mirrors the queryHandlerTracer name so traces
// and metrics for the same handler share an instrumentation scope.
const cloudResourceListMeterName = "eshu/go/internal/query"

// cloudResourceListInstruments holds the duration histogram and error counter
// for GET /api/v0/cloud/resources. The instrument names match the canonical
// definitions in go/internal/telemetry/instruments.go; the query package
// records to them through the global meter provider that cmd/api installs via
// telemetry.NewProviders, the same way queryHandlerTracer pulls the global
// tracer provider.
type cloudResourceListInstruments struct {
	duration metric.Float64Histogram
	errors   metric.Int64Counter
}

var (
	cloudResourceListInstrumentsOnce sync.Once
	cloudResourceListInstrumentsVal  *cloudResourceListInstruments
)

// cloudResourceListMetrics returns the process-wide cloud-resource list
// instruments, registering them once against the global meter. When
// registration fails (for example before a meter provider is installed in a
// unit test) it returns an instruments value with nil fields; callers must
// nil-check before recording.
func cloudResourceListMetrics() *cloudResourceListInstruments {
	cloudResourceListInstrumentsOnce.Do(func() {
		meter := otel.Meter(cloudResourceListMeterName)
		inst := &cloudResourceListInstruments{}
		neo4jQueryBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
		if hist, err := meter.Float64Histogram(
			"eshu_dp_cloud_resource_list_duration_seconds",
			metric.WithDescription("Cloud resource list query duration for GET /api/v0/cloud/resources"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(neo4jQueryBuckets...),
		); err == nil {
			inst.duration = hist
		}
		if counter, err := meter.Int64Counter(
			"eshu_dp_cloud_resource_list_errors_total",
			metric.WithDescription("Cloud resource list query errors for GET /api/v0/cloud/resources"),
		); err == nil {
			inst.errors = counter
		}
		cloudResourceListInstrumentsVal = inst
	})
	return cloudResourceListInstrumentsVal
}

// recordCloudResourceList records the query duration and, when failed, a single
// error event labeled with a bounded outcome reason. The reason values are a
// fixed closed set ("ok", "query_error") so the metric stays dashboard-safe.
func recordCloudResourceList(ctx context.Context, start time.Time, failed bool) {
	inst := cloudResourceListMetrics()
	if inst == nil {
		return
	}
	outcome := "ok"
	if failed {
		outcome = "query_error"
	}
	if inst.duration != nil {
		inst.duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("outcome", outcome),
		))
	}
	if failed && inst.errors != nil {
		inst.errors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", outcome),
		))
	}
}
