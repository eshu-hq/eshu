// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Span attributes recording one scoped tag-history page's grant filtering
// (#6564). They are counts and a boolean only, never digests or repository
// ids, so they stay bounded.
const (
	spanAttrTagHistoryGrantFiltered        = "eshu.query.tag_history.grant_filtered"
	spanAttrTagHistoryKeptCount            = "eshu.query.tag_history.grant_kept_count"
	spanAttrTagHistoryUngrantedCount       = "eshu.query.tag_history.withheld_ungranted_count"
	spanAttrTagHistoryUnattributedCount    = "eshu.query.tag_history.withheld_unattributed_count"
	spanAttrTagHistoryPreviousBlankedCount = "eshu.query.tag_history.previous_digest_blanked_count"
	spanAttrTagHistoryRefillReads          = "eshu.query.tag_history.refill_reads"
	spanAttrTagHistoryRefillCapReached     = "eshu.query.tag_history.refill_read_cap_reached"
)

// annotateTagHistoryRefill records how much work one scoped page's refill did.
// reads is the number of taghistory.Cypher windows the request consumed, and
// capReached says the page stopped on taghistory.MaxRefillReads rather than on a
// full page or the end of the history -- the signal an operator needs at 3 AM
// to tell "this caller's grant covers a thin slice of a busy tag" from "the
// route is slow", since a capped page is also the page that pays the most
// BUILT_FROM lookups. Both are bounded scalars, never digests or repository
// ids.
func annotateTagHistoryRefill(span trace.Span, reads int, capReached bool) {
	if span == nil {
		return
	}
	span.SetAttributes(
		attribute.Int(spanAttrTagHistoryRefillReads, reads),
		attribute.Bool(spanAttrTagHistoryRefillCapReached, capReached),
	)
}

// Bounded disposition values for the scoped-rows counter
// (eshu_dp_query_container_image_tag_history_scoped_rows_total).
const (
	tagHistoryDispositionKept                  = "kept"
	tagHistoryDispositionWithheldUngranted     = "withheld_ungranted"
	tagHistoryDispositionWithheldUnattributed  = "withheld_unattributed"
	tagHistoryDispositionPreviousDigestBlanked = "previous_digest_blanked"
)

// annotateTagHistoryGrantCounts writes the bounded filter counts one scoped
// page's grant filter produced (taghistory.GrantCounts) onto the handler span.
func annotateTagHistoryGrantCounts(span trace.Span, counts taghistory.GrantCounts) {
	if span == nil {
		return
	}
	span.SetAttributes(
		attribute.Bool(spanAttrTagHistoryGrantFiltered, true),
		attribute.Int(spanAttrTagHistoryKeptCount, counts.Kept),
		attribute.Int(spanAttrTagHistoryUngrantedCount, counts.WithheldUngranted),
		attribute.Int(spanAttrTagHistoryUnattributedCount, counts.WithheldUnattributed),
		attribute.Int(spanAttrTagHistoryPreviousBlankedCount, counts.PreviousDigestBlanked),
	)
}

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
	tagHistoryScopedRows           metric.Int64Counter
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
		tagHistoryScopedRows, err = meter.Int64Counter(
			"eshu_dp_query_container_image_tag_history_scoped_rows_total",
			metric.WithDescription("Container image tag history rows a scoped caller's BUILT_FROM grant filter kept, withheld, or blanked, by disposition"),
		)
		if err != nil {
			tagHistoryScopedRows = nil
		}
	})
}

// recordTagHistoryScopedRows adds one scoped page's grant-filter outcome to the
// scoped-rows counter. The disposition label is bounded to kept,
// withheld_ungranted, withheld_unattributed, and previous_digest_blanked, so an
// operator can separate rows another tenant owns from rows no BUILT_FROM edge
// attributes to anyone -- the coverage cost of binding tag history through
// BUILT_FROM (#6564). Zero counts are skipped rather than recorded as zero
// increments.
func recordTagHistoryScopedRows(ctx context.Context, counts taghistory.GrantCounts) {
	initTagHistoryQueryInstruments()
	if tagHistoryScopedRows == nil {
		return
	}
	for _, entry := range []struct {
		disposition string
		value       int
	}{
		{tagHistoryDispositionKept, counts.Kept},
		{tagHistoryDispositionWithheldUngranted, counts.WithheldUngranted},
		{tagHistoryDispositionWithheldUnattributed, counts.WithheldUnattributed},
		{tagHistoryDispositionPreviousDigestBlanked, counts.PreviousDigestBlanked},
	} {
		if entry.value == 0 {
			continue
		}
		tagHistoryScopedRows.Add(
			ctx, int64(entry.value),
			metric.WithAttributes(
				attribute.String("disposition", entry.disposition),
				attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
			),
		)
	}
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
