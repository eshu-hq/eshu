// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Tool values for the HybridRerankAttrTool attribute: the read surface whose
// page the in-request hybrid re-rank ordered. The set is closed.
const (
	HybridRerankToolFindCode            = "find_code"
	HybridRerankToolSearchEntityContent = "search_entity_content"
	HybridRerankToolSearchFileContent   = "search_file_content"
)

// Attribute keys on the telemetry.SpanQueryHybridRerank span. Each carries a
// bounded value: a tool from the closed set above, a row count of at most one
// page (200), and a boolean.
const (
	// HybridRerankAttrTool names the read surface whose page was ranked.
	HybridRerankAttrTool attribute.Key = "search.rerank.tool"
	// HybridRerankAttrRows is the number of page rows handed to the ranker.
	HybridRerankAttrRows attribute.Key = "search.rerank.rows"
	// HybridRerankAttrApplied reports whether the ranker reordered the page
	// (true) or kept the lexical order (false).
	HybridRerankAttrApplied attribute.Key = "search.rerank.applied"
)

// StartHybridRerankSpan opens the telemetry.SpanQueryHybridRerank stage span as
// a child of ctx and tags it with the tool and the number of page rows. The
// span covers the ranker's projection, request-local index build, and ranking,
// so its duration is the CPU cost the stage adds to one request (#7746).
//
// It returns the span's context and a finish function that records whether the
// ranker reordered the page and ends the span. The caller must call finish
// exactly once. The tracer is a parameter for the same reason as in
// StartHandlerSpanWith: a test's recording tracer must reach the span.
func StartHybridRerankSpan(
	ctx context.Context,
	tracer trace.Tracer,
	tool string,
	rows int,
) (context.Context, func(applied bool)) {
	ctx, span := tracer.Start(ctx, telemetry.SpanQueryHybridRerank)
	span.SetAttributes(
		HybridRerankAttrTool.String(tool),
		HybridRerankAttrRows.Int(rows),
	)
	return ctx, func(applied bool) {
		span.SetAttributes(HybridRerankAttrApplied.Bool(applied))
		span.End()
	}
}
