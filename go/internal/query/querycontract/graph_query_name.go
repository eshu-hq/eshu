// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"time"
)

// graphQueryNameContextKey is the unexported context key carrying a bounded,
// low-cardinality caller name for the current graph read across the call
// boundary from a handler to Neo4jReader.runRead
// (go/internal/query/neo4j_read_policy.go). It exists so the
// "bounded graph read completed with warning" log and its matching span can
// name which query hit the deadline (issue #7006's telemetry gap), without
// adding a parameter to every querycontract.GraphQuery.Run/RunSingle call
// site.
type graphQueryNameContextKey struct{}

// DefaultGraphQueryName is what GraphQueryNameFromContext returns when no
// caller set a name, so the telemetry attribute is always present rather than
// sometimes absent.
const DefaultGraphQueryName = "unnamed"

// WithGraphQueryName returns ctx wrapped with name attached, so the eventual
// graph read this context reaches can be attributed in telemetry. name should
// be a bounded, low-cardinality route/handler identifier (e.g.
// "code_quality.complexity_list"), never raw Cypher text, an entity id, or
// other high-cardinality or private value.
func WithGraphQueryName(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, graphQueryNameContextKey{}, name)
}

// GraphQueryNameFromContext returns the name WithGraphQueryName attached to
// ctx, or DefaultGraphQueryName when none was set.
func GraphQueryNameFromContext(ctx context.Context) string {
	if name, ok := ctx.Value(graphQueryNameContextKey{}).(string); ok && name != "" {
		return name
	}
	return DefaultGraphQueryName
}

// DefaultGraphReadTimeout is the bounded-read deadline a single graph
// statement gets via Neo4jReader.runRead (go/internal/query/neo4j_read_policy.go's
// defaultGraphReadTimeout, which is defined in terms of this constant so the
// two never drift). A caller that issues multiple sequential graph reads to
// answer one logical request -- e.g. a per-label anchor-resolution loop --
// must derive ONE shared deadline from this constant before the loop, never
// let each read claim its own fresh window: Neo4jReader.runRead creates its
// own context.WithTimeout(ctx, readTimeout) per call, so an unbounded outer
// ctx lets N sequential reads cost up to N times this budget instead of one
// (issue #7006 review finding).
const DefaultGraphReadTimeout = 10 * time.Second

// WithBoundedGraphReadDeadline returns ctx wrapped with a deadline of
// DefaultGraphReadTimeout, and the cancel func the caller must defer. Because
// context.WithTimeout resolves to the EARLIER of the parent's and the new
// deadline, wrapping an already-short-deadlined ctx (e.g. one bounded by an
// MCP dispatch timeout, or a test) keeps the shorter deadline -- this call
// only ever tightens the budget, never extends it. Use this once before a
// loop of sequential graph reads that logically answer one request (e.g.
// GetEntityContext's or getRelationships' per-label anchor loop), so the
// total cost across every iteration is bounded by the same single budget a
// lone graph statement gets.
func WithBoundedGraphReadDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, DefaultGraphReadTimeout)
}
