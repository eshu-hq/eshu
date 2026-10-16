// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Span attributes recorded on the infra search request span.
const (
	// SpanAttrPath is the statement that served the search.
	SpanAttrPath = "eshu.infra_search_path"
	// SpanAttrFallbackReason is why the legacy statement served it.
	SpanAttrFallbackReason = "eshu.infra_search_fallback_reason"
)

// Reader runs one infra search on the statement Select picks. A nil *Reader
// always runs the legacy statement.
type Reader struct {
	// Enabled is ESHU_INFRA_SEARCH_KEY_READ.
	Enabled bool
	// Readiness is the cached index readiness; nil is never ready.
	Readiness *Readiness
	// Logger receives one warning per full-text read that fell back. Nil
	// disables it.
	Logger *slog.Logger
	// Instruments records eshu_dp_infra_search_path_total. Nil disables it.
	Instruments *telemetry.Instruments
}

// Request is one search, already built in its legacy form by the handler.
type Request struct {
	// Neo4j is true when the graph backend is Neo4j.
	Neo4j bool
	// Query is the trimmed free-text query; empty means a structured search.
	Query string
	// Labels is the candidate label set (all infra labels or a category's).
	Labels []string
	// WhereExtra is the legacy WHERE suffix: the free-text or "::" equality
	// predicate and the structured filters, without any grant clause.
	WhereExtra string
	// Scoped selects ScopedStatement; ScopePredicate is its grant predicate.
	Scoped bool
	// ScopePredicate is the hoisted Neo4j grant predicate over n.
	ScopePredicate string
	// ReturnExprs are the projected "expr as column" pairs.
	ReturnExprs []string
	// Legacy is the legacy statement, run unchanged on the legacy path.
	Legacy string
	// Params are the legacy statement's params. Rows never mutates them.
	Params map[string]any
}

// Rows runs the selected statement and returns its rows. When the full-text
// read fails while ctx is still live, it runs the legacy statement for this
// request instead, so a caller never gets partial rows or a full-text error.
// It records the chosen path on the span in ctx and on the path counter.
func (r *Reader) Rows(ctx context.Context, run Runner, req Request) ([]map[string]any, error) {
	enabled := r != nil && r.Enabled
	var state State
	if r != nil {
		state = r.Readiness.Current()
	}
	path, reason := Select(enabled, req.Neo4j, req.Query, state)
	if path == PathFulltext {
		rows, err := run.Run(ctx, r.statement(req), r.params(req))
		if err == nil {
			r.observe(ctx, PathFulltext, ReasonReady)
			return rows, nil
		}
		if ctx.Err() != nil {
			r.observe(ctx, PathFulltext, ReasonFTError)
			return nil, err
		}
		// No raw driver error here: it can echo statement text. The graph
		// reader already logged the failed read with its bounded class.
		if r.Logger != nil {
			r.Logger.WarnContext(ctx, "infra search full-text read failed; serving the legacy statement",
				telemetry.EventAttr("query.infra_search_key.fulltext_fallback"),
				"search_path", string(PathLegacy),
				"reason", string(ReasonFTError),
			)
		}
		path, reason = PathLegacy, ReasonFTError
	}
	r.observe(ctx, path, reason)
	return run.Run(ctx, req.Legacy, req.Params)
}

// statement builds the full-text statement for req.
func (*Reader) statement(req Request) string {
	if req.Scoped {
		return ScopedStatement(req.Labels, req.WhereExtra, req.ScopePredicate, req.ReturnExprs)
	}
	return UnscopedStatement(req.Labels, req.WhereExtra, req.ReturnExprs)
}

// params copies the legacy params and adds the Lucene query, so the caller's
// map, which the fallback reuses, never carries the full-text param.
func (*Reader) params(req Request) map[string]any {
	params := make(map[string]any, len(req.Params)+1)
	for k, v := range req.Params {
		params[k] = v
	}
	params[LuceneParam] = Lucene(req.Query)
	return params
}

func (r *Reader) observe(ctx context.Context, path Path, reason Reason) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String(SpanAttrPath, string(path)))
	if path == PathLegacy {
		span.SetAttributes(attribute.String(SpanAttrFallbackReason, string(reason)))
	}
	if r == nil || r.Instruments == nil || r.Instruments.InfraSearchPath == nil {
		return
	}
	r.Instruments.InfraSearchPath.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrSearchPath(string(path)),
		telemetry.AttrReason(string(reason)),
	))
}
