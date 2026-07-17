// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestPersistedSemanticSearchAnnotatesMissThenHit(t *testing.T) {
	t.Parallel()

	hybrid, _, _, _, _ := newSemanticSearchCacheTestHybrid(t, 2)
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracer := provider.Tracer("semantic-search-cache-test")
	query := semanticSearchCacheTestQuery("refund")

	for _, name := range []string{"miss", "hit"} {
		ctx, span := tracer.Start(context.Background(), name)
		if _, err := hybrid.Search(ctx, query); err != nil {
			span.End()
			t.Fatalf("Search() %s error = %v", name, err)
		}
		span.End()
	}

	spans := recorder.Ended()
	if got, want := len(spans), 2; got != want {
		t.Fatalf("ended spans = %d, want %d", got, want)
	}
	for i, want := range []string{semanticSearchCacheStateMiss, semanticSearchCacheStateHit} {
		if got := semanticSearchSpanAttribute(spans[i].Attributes(), "search.index_cache"); got != want {
			t.Fatalf("span %d search.index_cache = %q, want %q", i, got, want)
		}
	}
}

func semanticSearchSpanAttribute(attributes []attribute.KeyValue, key string) string {
	for _, candidate := range attributes {
		if string(candidate.Key) == key {
			return candidate.Value.AsString()
		}
	}
	return ""
}
