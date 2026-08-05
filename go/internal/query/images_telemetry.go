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

// imageQueryMeterName scopes the lazily registered image-list instruments to
// this package, mirroring cloudResourceListMeterName in
// cloud_resources_metrics.go: the query package is not handed a
// *telemetry.Instruments, so the two image-list instruments are registered
// lazily here and recorded directly from the handler.
const imageQueryMeterName = "eshu/go/internal/query"

var (
	imageQueryInstrumentsOnce sync.Once
	imageListDuration         metric.Float64Histogram
	imageListErrors           metric.Int64Counter
)

// imageListBuckets bound the image-list handler latency histogram. The list is
// a single bounded label scan over (:ContainerImage) capped at limit+1 rows, so
// the buckets stay in the sub-second to low-second range an operator expects.
var imageListBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// initImageQueryInstruments registers the image-list duration histogram and the
// error counter exactly once. Registration errors leave the instruments nil and
// recording becomes a no-op so a telemetry pipeline fault never fails the read.
//
// The meter is fetched from the current global provider inside the once (not
// cached in a package var) so a test that installs its own meter provider
// before the first record observes the counter regardless of test ordering
// (mirrors the meter fetch inside initTagHistoryQueryInstruments in
// tag_history_telemetry.go and cloudResourceListMetrics in
// cloud_resources_metrics.go). A meter obtained
// once at package-init time, before any provider is installed, would bind
// permanently to whichever provider first calls otel.SetMeterProvider in the
// process — the OTel global proxy only resolves each such cached meter's
// delegate on that first call
// (go.opentelemetry.io/otel/internal/global.meterProvider.setDelegate: "It is
// guaranteed by the caller that this happens only once") — so a later test
// installing its own reader would silently record onto the earlier reader
// instead.
func initImageQueryInstruments() {
	imageQueryInstrumentsOnce.Do(func() {
		meter := otel.Meter(imageQueryMeterName)
		var err error
		imageListDuration, err = meter.Float64Histogram(
			"eshu_dp_query_image_list_duration_seconds",
			metric.WithDescription("Container image list handler duration"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(imageListBuckets...),
		)
		if err != nil {
			imageListDuration = nil
		}
		imageListErrors, err = meter.Int64Counter(
			"eshu_dp_query_image_list_errors_total",
			metric.WithDescription("Container image list handler errors by reason"),
		)
		if err != nil {
			imageListErrors = nil
		}
	})
}

// recordImageListDuration observes one image-list handler invocation. The
// outcome label is low cardinality (ok, invalid_request, unsupported_capability,
// backend_unavailable, query_error) so it is safe as a metric dimension.
func recordImageListDuration(ctx context.Context, start time.Time, outcome string) {
	initImageQueryInstruments()
	if imageListDuration == nil {
		return
	}
	imageListDuration.Record(
		ctx, time.Since(start).Seconds(),
		metric.WithAttributes(
			attribute.String("outcome", outcome),
			attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
		),
	)
}

// recordImageListError increments the image-list error counter with a bounded
// reason label so an operator can distinguish bad input from backend faults.
func recordImageListError(ctx context.Context, reason string) {
	initImageQueryInstruments()
	if imageListErrors == nil {
		return
	}
	imageListErrors.Add(
		ctx, 1,
		metric.WithAttributes(
			attribute.String("reason", reason),
			attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
		),
	)
}
