// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// semanticSearchDegradedReasonNoEmbedder is the only current degradation reason:
// no embedder/vector ranking was available, so the request was served by BM25
// (hybrid) or refused (semantic). It is a bounded metric label value.
const semanticSearchDegradedReasonNoEmbedder = "no_embedder"

// The query package is not handed a *telemetry.Instruments (see
// images_telemetry.go), so the degraded-search counter is registered lazily here
// and recorded directly from the handler chokepoint. The meter is fetched from
// the current global provider inside the once (not cached at package init) so a
// test that installs its own meter provider before the first record observes the
// counter regardless of ordering.
var (
	semanticSearchInstrumentsOnce sync.Once
	searchHybridDegradedTotal     metric.Int64Counter
)

// initSemanticSearchInstruments registers the degraded-search counter exactly
// once. A registration error leaves the instrument nil so recording becomes a
// no-op and a telemetry pipeline fault never fails a read.
func initSemanticSearchInstruments() {
	semanticSearchInstrumentsOnce.Do(func() {
		var err error
		searchHybridDegradedTotal, err = otel.Meter("eshu/go/internal/query").Int64Counter(
			"eshu_dp_search_hybrid_degraded_total",
			metric.WithDescription(
				"Semantic/hybrid search requests served without semantic ranking "+
					"(degraded to BM25, or refused) by query_type and reason. Expected, "+
					"not an error, in no-embedder mode.",
			),
		)
		if err != nil {
			searchHybridDegradedTotal = nil
		}
	})
}

// semanticSearchDegradation classifies a handler retrieval_state into the bounded
// degraded signal. A degradation is a request that asked for semantic ranking
// (hybrid or semantic) but was served without it; an explicit keyword request and
// a fully active hybrid/semantic run are not degradations. query_type and reason
// are bounded label values; both are empty when the state is not degraded.
func semanticSearchDegradation(retrievalState string) (degraded bool, queryType, reason string) {
	switch retrievalState {
	case "hybrid_degraded":
		return true, "hybrid", semanticSearchDegradedReasonNoEmbedder
	case "semantic_unavailable":
		return true, "semantic", semanticSearchDegradedReasonNoEmbedder
	default:
		return false, "", ""
	}
}

// semanticSearchDegradationForError classifies the semantic-mode no-embedder
// failure (a 503 served before any retrieval_state is produced) as a degradation
// so the signal also captures the provider-unavailable path, not only the
// hybrid-degraded success path.
func semanticSearchDegradationForError(err error) (degraded bool, queryType, reason string) {
	if err != nil && strings.Contains(err.Error(), "requires an embedder") {
		return true, "semantic", semanticSearchDegradedReasonNoEmbedder
	}
	return false, "", ""
}

// recordSemanticSearchDegraded increments the degraded-search counter with bounded
// query_type and reason labels.
func recordSemanticSearchDegraded(ctx context.Context, queryType, reason string) {
	initSemanticSearchInstruments()
	if searchHybridDegradedTotal == nil {
		return
	}
	searchHybridDegradedTotal.Add(
		ctx, 1,
		metric.WithAttributes(
			attribute.String("query_type", queryType),
			attribute.String("reason", reason),
			attribute.String("service.namespace", telemetry.DefaultServiceNamespace),
		),
	)
}

// annotateSemanticSearchDegraded records the degraded signal (span attributes plus
// the counter) for a completed retrieval. It always stamps the retrieval_state and
// degraded flag on the span so an operator can see the served mode of every
// request, and increments the counter only when the request was actually degraded.
func annotateSemanticSearchDegraded(ctx context.Context, span trace.Span, retrievalState string) {
	degraded, queryType, reason := semanticSearchDegradation(retrievalState)
	span.SetAttributes(
		attribute.String("search.retrieval_state", retrievalState),
		attribute.Bool("search.degraded", degraded),
	)
	if !degraded {
		return
	}
	span.SetAttributes(attribute.String("search.degraded_reason", reason))
	recordSemanticSearchDegraded(ctx, queryType, reason)
}

// annotateSemanticSearchDegradedError records the degraded signal for the
// semantic-mode no-embedder 503 path, where no retrieval_state is produced.
func annotateSemanticSearchDegradedError(ctx context.Context, span trace.Span, err error) {
	degraded, queryType, reason := semanticSearchDegradationForError(err)
	if !degraded {
		return
	}
	span.SetAttributes(
		attribute.String("search.retrieval_state", "semantic_unavailable"),
		attribute.Bool("search.degraded", true),
		attribute.String("search.degraded_reason", reason),
	)
	recordSemanticSearchDegraded(ctx, queryType, reason)
}
