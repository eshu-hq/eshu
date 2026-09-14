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
// reads is the number of keyset windows the request consumed (each a fixed
// taghistory.MaxLimit rows, not limit rows), and
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

// Bounded outcome values for the scoped-pages counter
// (eshu_dp_query_container_image_tag_history_scoped_pages_total). They describe
// one PAGE, never its rows: see recordTagHistoryScopedPage for why the per-row
// withheld counts stay off this public surface.
const (
	tagHistoryScopedPageComplete       = "complete"
	tagHistoryScopedPageReadCapReached = "read_cap_reached"
)

// annotateTagHistoryGrantCounts writes the bounded filter counts one scoped
// page's grant filter produced (taghistory.GrantCounts) onto the handler span.
//
// The span is the ONLY place the two withheld counts are published, and that is
// deliberate (#6564). It reaches an operator through the OTLP trace exporter,
// not through the unauthenticated /metrics scrape a scoped caller can also
// reach, and it carries the per-request context -- which image_ref, how many
// refill windows -- that a fleet-wide counter could never attribute. See
// recordTagHistoryScopedPage for the side channel this split exists to close.
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

// tagHistoryOutcomeCursorUnavailable is the bounded outcome label for the one
// degraded state this route has: the deployment holds no cursor sealing key
// (ESHU_AUTH_SECRET_ENC_KEY(_FILE)), so a grant-filtered caller's page is
// served correct and short with next_cursor omitted, and a grant-filtered
// request that carries a cursor is refused 503.
//
// It is deliberately NOT "ok" and NOT "query_error": the read succeeded and
// nothing is wrong with the backend, but paging is unavailable for scoped
// callers until an operator sets the variable. That is the signal an operator
// needs at 3 AM to tell this apart from a caller that simply reached the end of
// its history, and it is the only way a missing mount on the standalone MCP
// server shows up in metrics rather than only in one startup log line.
const tagHistoryOutcomeCursorUnavailable = "cursor_unavailable"

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
	tagHistoryScopedPages          metric.Int64Counter
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
		tagHistoryScopedPages, err = meter.Int64Counter(
			"eshu_dp_query_container_image_tag_history_scoped_pages_total",
			metric.WithDescription("Container image tag history pages served to a grant-filtered caller, by whether the refill loop reached its per-request read cap"),
		)
		if err != nil {
			tagHistoryScopedPages = nil
		}
	})
}

// recordTagHistoryScopedPage counts one grant-filtered page by whether its
// refill loop ran out of read budget (#6564).
//
// It publishes a PAGE outcome and deliberately NOT the per-row withheld counts
// the grant filter produced. /metrics is unauthenticated -- a literal entry in
// publicHTTPPaths (auth.go), which the middleware honours before any token
// handling -- and it is mounted on the same admin mux the API surface is served
// through, so a scoped caller can scrape it. A counter carrying
// withheld_ungranted and withheld_unattributed increments, labelled only by
// disposition and service.namespace, would let that caller recover its own
// page's withheld count from a before/after scrape on a quiet deployment. On a
// filled page that count is precisely what the response body declines to state,
// so publishing it here would re-disclose one surface over exactly what the body
// was built to withhold.
//
// Both outcomes below are already in the caller's hands. Every scoped caller
// reads grant_filtered: true in its own body, and read_cap_reached is derivable
// from that same body: taghistory.RefillScopedPage sets CapReached exactly when
// it returns a truncated page holding fewer than limit rows, and
// taghistory.ScopedTruthReason spells that residue out in the truth envelope.
// So the series answers the 3 AM question -- what share of scoped tag-history
// pages are exhausting the refill budget, which is the signal that a caller's
// grant covers a thin slice of a busy tag rather than that the route is slow --
// without carrying a number anyone learns something new from.
//
// The denominator is pages that actually ran the filter. A scoped caller with
// no grants at all is answered without a graph read and is not counted here;
// its page withheld nothing because nothing was read.
func recordTagHistoryScopedPage(ctx context.Context, capReached bool) {
	initTagHistoryQueryInstruments()
	if tagHistoryScopedPages == nil {
		return
	}
	outcome := tagHistoryScopedPageComplete
	if capReached {
		outcome = tagHistoryScopedPageReadCapReached
	}
	tagHistoryScopedPages.Add(
		ctx, 1,
		metric.WithAttributes(
			attribute.String("outcome", outcome),
			attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
		),
	)
}

// recordTagHistoryDuration observes one tag-history handler invocation. The
// outcome label is low cardinality (ok, invalid_request, unsupported_capability,
// backend_unavailable, cursor_unavailable, query_error) so it is safe as a
// metric dimension.
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
