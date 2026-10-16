// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestContentHybridRerankEmitsStageSpan pins the query.hybrid_rerank stage span
// on both content re-rank paths: one span per pass that reaches the ranking
// work, with the tool, the page row count, and applied matching what the
// ranker returned. A file page with no bodies still enters the pass and
// records applied=false (#7746).
func TestContentHybridRerankEmitsStageSpan(t *testing.T) {
	t.Parallel()

	const count = 3
	entities := make([]querycontract.EntityContent, 0, count)
	files := make([]querycontract.FileContent, 0, count)
	bodyless := make([]querycontract.FileContent, 0, count)
	for i := 0; i < count; i++ {
		entities = append(entities, querycontract.EntityContent{
			EntityID: fmt.Sprintf("entity-%d", i), RepoID: maxPageRepoID, EntityType: "function",
			EntityName: fmt.Sprintf("handler%d", i), SourceCache: maxPageBody(i, count),
		})
		path := fmt.Sprintf("service/handler_%d.go", i)
		files = append(files, querycontract.FileContent{RepoID: maxPageRepoID, RelativePath: path, Content: maxPageBody(i, count)})
		bodyless = append(bodyless, querycontract.FileContent{RepoID: maxPageRepoID, RelativePath: path})
	}

	cases := []struct {
		name    string
		tool    string
		applied bool
		run     func(*ContentHybridRanker) bool
	}{
		{
			name: "entities applied", tool: tracing.HybridRerankToolSearchEntityContent, applied: true,
			run: func(r *ContentHybridRanker) bool {
				_, ok := r.RerankEntities(context.Background(), maxPageRepoID, maxPageQuery, entities)
				return ok
			},
		},
		{
			name: "files applied", tool: tracing.HybridRerankToolSearchFileContent, applied: true,
			run: func(r *ContentHybridRanker) bool {
				_, ok := r.RerankFiles(context.Background(), maxPageRepoID, maxPageQuery, files)
				return ok
			},
		},
		{
			name: "bodyless files fall back", tool: tracing.HybridRerankToolSearchFileContent, applied: false,
			run: func(r *ContentHybridRanker) bool {
				_, ok := r.RerankFiles(context.Background(), maxPageRepoID, maxPageQuery, bodyless)
				return ok
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ranker := NewContentHybridRanker(true)
			ranker.tracer = provider.Tracer("content-hybrid-rerank-test")

			if got := tc.run(ranker); got != tc.applied {
				t.Fatalf("applied = %v, want %v", got, tc.applied)
			}
			ended := recorder.Ended()
			if len(ended) != 1 || ended[0].Name() != telemetry.SpanQueryHybridRerank {
				t.Fatalf("ended spans = %d, want one %s span", len(ended), telemetry.SpanQueryHybridRerank)
			}
			seen := map[attribute.Key]attribute.Value{}
			for _, attr := range ended[0].Attributes() {
				seen[attr.Key] = attr.Value
			}
			if got := seen[tracing.HybridRerankAttrTool].AsString(); got != tc.tool {
				t.Errorf("tool = %q, want %q", got, tc.tool)
			}
			if got := seen[tracing.HybridRerankAttrRows].AsInt64(); got != count {
				t.Errorf("rows = %d, want %d", got, count)
			}
			if value, ok := seen[tracing.HybridRerankAttrApplied]; !ok || value.AsBool() != tc.applied {
				t.Errorf("applied attribute = %v (present %v), want %v", value.AsBool(), ok, tc.applied)
			}
		})
	}
}
