// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// tagHistoryQueryMeterName scopes the lazily registered tag-history
// instruments to this package, mirroring cloudResourceListMeterName in
// cloud_resources_metrics.go: the query package is not handed a
// *telemetry.Instruments, so the two tag-history instruments are registered
// lazily here and recorded directly from the handler.
const tagHistoryQueryMeterName = "eshu/go/internal/query"

var (
	tagHistoryQueryInstrumentsOnce sync.Once
	tagHistoryDuration             metric.Float64Histogram
	tagHistoryErrors               metric.Int64Counter
)

// tagHistoryBuckets bound the tag-history handler latency histogram. The read
// is a single indexed image_ref-anchored lookup capped at limit+1 rows, so the
// buckets stay in the sub-second to low-second range an operator expects.
var tagHistoryBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// initTagHistoryQueryInstruments registers the tag-history duration histogram
// and the error counter exactly once. Registration errors leave the
// instruments nil and recording becomes a no-op so a telemetry pipeline fault
// never fails the read.
//
// The meter is fetched from the current global provider inside the once (not
// cached in a package var) so a test that installs its own meter provider
// before the first record observes the counter regardless of test ordering
// (mirrors semanticSearchInstrumentsOnce in semantic_search_telemetry.go and
// cloudResourceListMetrics in cloud_resources_metrics.go). A meter obtained
// once at package-init time, before any provider is installed, would bind
// permanently to whichever provider first calls otel.SetMeterProvider in the
// process — the OTel global proxy only resolves each such cached meter's
// delegate on that first call
// (go.opentelemetry.io/otel/internal/global.meterProvider.setDelegate: "It is
// guaranteed by the caller that this happens only once") — so a later test
// installing its own reader would silently record onto the earlier reader
// instead.
func initTagHistoryQueryInstruments() {
	tagHistoryQueryInstrumentsOnce.Do(func() {
		meter := otel.Meter(tagHistoryQueryMeterName)
		var err error
		tagHistoryDuration, err = meter.Float64Histogram(
			"eshu_dp_query_container_image_tag_history_duration_seconds",
			metric.WithDescription("Container image tag history handler duration"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(tagHistoryBuckets...),
		)
		if err != nil {
			tagHistoryDuration = nil
		}
		tagHistoryErrors, err = meter.Int64Counter(
			"eshu_dp_query_container_image_tag_history_errors_total",
			metric.WithDescription("Container image tag history handler errors by reason"),
		)
		if err != nil {
			tagHistoryErrors = nil
		}
	})
}

// recordTagHistoryDuration observes one tag-history handler invocation. The
// outcome label is low cardinality (ok, invalid_request, unsupported_capability,
// backend_unavailable, query_error) so it is safe as a metric dimension.
func recordTagHistoryDuration(ctx context.Context, start time.Time, outcome string) {
	initTagHistoryQueryInstruments()
	if tagHistoryDuration == nil {
		return
	}
	tagHistoryDuration.Record(
		ctx, time.Since(start).Seconds(),
		metric.WithAttributes(
			attribute.String("outcome", outcome),
			attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
		),
	)
}

// recordTagHistoryError increments the tag-history error counter with a
// bounded reason label so an operator can distinguish bad input from backend
// faults.
func recordTagHistoryError(ctx context.Context, reason string) {
	initTagHistoryQueryInstruments()
	if tagHistoryErrors == nil {
		return
	}
	tagHistoryErrors.Add(
		ctx, 1,
		metric.WithAttributes(
			attribute.String("reason", reason),
			attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
		),
	)
}
