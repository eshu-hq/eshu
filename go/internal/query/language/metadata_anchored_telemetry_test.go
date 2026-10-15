// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const languageQueryMetadataRowsMetric = "eshu_dp_language_query_metadata_rows_total"

// metadataObservation is what one enrichment wrote to the request span and
// the metadata-rows counter.
type metadataObservation struct {
	spanAttrs map[string]attribute.Value
	// counts is keyed "metadata_path/outcome".
	counts map[string]int64
}

// observeMetadataEnrichment runs one queryByLanguage under a recording span
// and a manual metric reader and returns what the enrichment recorded.
func observeMetadataEnrichment(t *testing.T, handler *Handler, label string) metadataObservation {
	t.Helper()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("language-query-metadata-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler.Instruments = instruments

	spans := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	ctx, span := tracerProvider.Tracer("language-query-metadata-test").Start(context.Background(), "POST /api/v0/code/language-query")
	if _, _, err := handler.queryByLanguage(ctx, "php", label, "", "", 10, anchoredGrant()); err != nil {
		t.Fatalf("queryByLanguage() error = %v", err)
	}
	span.End()

	obs := metadataObservation{spanAttrs: map[string]attribute.Value{}, counts: map[string]int64{}}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	for _, kv := range ended[0].Attributes() {
		obs.spanAttrs[string(kv.Key)] = kv.Value
	}

	var collected metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != languageQueryMetadataRowsMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				path, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionMetadataPath))
				outcome, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				obs.counts[path.AsString()+"/"+outcome.AsString()] += dp.Value
			}
		}
	}
	return obs
}

// TestLanguageMetadataEnrichmentRecordsTheAnchoredPath pins the operator
// signal for the id-anchored path: rows enriched and rows still missing
// metadata, on the request span and the counter.
func TestLanguageMetadataEnrichmentRecordsTheAnchoredPath(t *testing.T) {
	t.Parallel()

	graphRows := anchoredGraphRows("Function", 4)
	store := newAnchoredStore(graphRows, "Function")
	delete(store.byID, graphRows[3]["entity_id"].(string)) // one row has no content row
	handler := &Handler{Neo4j: &testutil.MockLanguageQueryGraphReader{Rows: graphRows}, Content: store}

	obs := observeMetadataEnrichment(t, handler, "Function")
	if got := obs.spanAttrs[spanAttrMetadataPath].AsString(); got != metadataPathAnchored {
		t.Fatalf("span %s = %q, want %q", spanAttrMetadataPath, got, metadataPathAnchored)
	}
	if got := obs.spanAttrs[spanAttrMetadataRowsEnriched].AsInt64(); got != 3 {
		t.Fatalf("span %s = %d, want 3", spanAttrMetadataRowsEnriched, got)
	}
	if got := obs.spanAttrs[spanAttrMetadataRowsMissing].AsInt64(); got != 1 {
		t.Fatalf("span %s = %d, want 1", spanAttrMetadataRowsMissing, got)
	}
	want := map[string]int64{"anchored/enriched": 3, "anchored/missing": 1}
	if len(obs.counts) != len(want) || obs.counts["anchored/enriched"] != 3 || obs.counts["anchored/missing"] != 1 {
		t.Fatalf("counter = %v, want %v", obs.counts, want)
	}
}

// TestLanguageMetadataEnrichmentRecordsThePageReadPath pins the same signal
// for a label left on the page read, where a disjoint page leaves every row
// missing -- the share an operator watches to see the page read's gap.
func TestLanguageMetadataEnrichmentRecordsThePageReadPath(t *testing.T) {
	t.Parallel()

	graphRows := anchoredGraphRows("Module", 4)
	store := newAnchoredStore(graphRows, "Module")
	handler := &Handler{Neo4j: &testutil.MockLanguageQueryGraphReader{Rows: graphRows}, Content: store}

	obs := observeMetadataEnrichment(t, handler, "Module")
	if got := obs.spanAttrs[spanAttrMetadataPath].AsString(); got != metadataPathPageRead {
		t.Fatalf("span %s = %q, want %q", spanAttrMetadataPath, got, metadataPathPageRead)
	}
	if got := obs.counts["page_read/missing"]; got != 4 {
		t.Fatalf("counter page_read/missing = %d, want 4 (counts %v)", got, obs.counts)
	}
	if got := obs.counts["page_read/enriched"]; got != 0 {
		t.Fatalf("counter page_read/enriched = %d, want 0", got)
	}
}

// TestLanguageQueryPageFitsTheIDLookupBound keeps the route's limit clamp and
// the id lookup's bound in step: a full page must never exceed the bound.
func TestLanguageQueryPageFitsTheIDLookupBound(t *testing.T) {
	t.Parallel()

	if languageQueryMaxLimit > taxonomy.MaxLanguageEntityIDLookup {
		t.Fatalf("languageQueryMaxLimit %d > taxonomy.MaxLanguageEntityIDLookup %d: a full page would be refused by the id lookup",
			languageQueryMaxLimit, taxonomy.MaxLanguageEntityIDLookup)
	}
}
