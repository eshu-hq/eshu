// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "context"

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
