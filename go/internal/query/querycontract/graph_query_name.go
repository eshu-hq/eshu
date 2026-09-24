// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"errors"
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
// "code_quality.complexity", "entity.context"), never raw Cypher text, an
// entity id, or other high-cardinality or private value.
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

// errBoundedGraphReadBudget is the context.WithTimeoutCause cause attached to
// the deadline WithBoundedGraphReadDeadline(For) creates, so
// Neo4jReader.runRead's graphReadResult (go/internal/query/neo4j_read_policy.go)
// can tell that a context's deadline came from the graph-read policy's own
// shared budget rather than an ordinary caller-imposed deadline (e.g. an MCP
// dispatch timeout, or a test's own context.WithTimeout). Both look identical
// from graphReadResult's parentCtx.Err() branch -- the parent context
// expired -- but only a deadline whose context.Cause is this sentinel IS the
// graph-read policy budget (#7006 review F1: without identifying which
// deadline actually fired, every timeout on a shared-budget loop route
// misclassified as graphReadOutcomeCallerDeadline -- no
// query.graph_read.warning log, no graph_query_name, and a raw
// context.DeadlineExceeded instead of the wrapped ErrGraphReadDeadline
// sentinel).
//
// A context.WithValue marker on the bounded ctx is NOT enough: it identifies
// which ctx CARRIES the budget, not which deadline FIRED. When a caller sets
// a shorter deadline outside the bounded ctx (e.g. context.WithTimeout(30ms)
// then WithBoundedGraphReadDeadlineFor(that ctx, 500ms)), the wrapped
// context's Err() still expires from the caller's shorter deadline, but a
// WithValue marker on the outer (bounded) ctx would still be visible and
// wrongly report a policy deadline for a deadline the graph-read policy
// never set (#7006 review round 4 F7). context.Cause resolves to the
// innermost expired deadline's own cause, so it distinguishes the two.
var errBoundedGraphReadBudget = errors.New("bounded graph read budget expired")

// IsBoundedGraphReadDeadline reports whether ctx's deadline is the one
// WithBoundedGraphReadDeadline(For) created, as opposed to an ancestor
// caller deadline that happened to expire first. See
// errBoundedGraphReadBudget.
func IsBoundedGraphReadDeadline(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errBoundedGraphReadBudget)
}

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
	return WithBoundedGraphReadDeadlineFor(ctx, DefaultGraphReadTimeout)
}

// WithBoundedGraphReadDeadlineFor is WithBoundedGraphReadDeadline with an
// explicit budget instead of the fixed DefaultGraphReadTimeout. Production
// code should call WithBoundedGraphReadDeadline; this variant exists so a
// test can reproduce the same parent-deadline-created-microseconds-before-
// readCtx-deadline race deterministically and fast, without waiting out the
// real 10s budget twice.
func WithBoundedGraphReadDeadlineFor(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, budget, errBoundedGraphReadBudget)
}
