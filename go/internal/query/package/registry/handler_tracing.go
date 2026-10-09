// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Fixed failure messages, one per route step. They are the only text a failed
// read puts in a response body: backend errors quote SQL, Cypher, hosts, and
// credentials (#7674). Each access-check message is shared by every scoped
// gate probe of its route, so a nonexistent anchor and an existing but
// ungranted one fail with the same body (no existence oracle).
const (
	packageRegistryPackagesQueryFailedMessage             = "package registry package query failed"
	packageRegistryPackageVersionCountsFailedMessage      = "package registry package version count query failed"
	packageRegistryPackagesNameLookupFailedMessage        = "package registry package name lookup failed"
	packageRegistryPackagesAccessCheckFailedMessage       = "package registry package access check failed"
	packageRegistryVersionsQueryFailedMessage             = "package registry version query failed"
	packageRegistryVersionsAccessCheckFailedMessage       = "package registry version access check failed"
	packageRegistryDependenciesQueryFailedMessage         = "package registry dependency query failed"
	packageRegistryDependenciesVersionLookupFailedMessage = "package registry dependency version lookup failed"
	packageRegistryDependenciesAccessCheckFailedMessage   = "package registry dependency access check failed"
	packageRegistryCorrelationsQueryFailedMessage         = "package registry correlation query failed"
	packageRegistryDependencyChainsQueryFailedMessage     = "package dependency chain query failed"
	packageRegistryAggregateCountFailedMessage            = "package registry package count query failed"
	packageRegistryAggregateInventoryFailedMessage        = "package registry package inventory query failed"
)

// packageregTracer is this package's tracer AND the seam its span tests swap.
// Mirrors root package query's handler_tracing.go: it must stay a
// package-local var, seeded from tracing.HandlerTracer, so a recording
// provider swapped in for this package's tests cannot change what any other
// family or root records, and two such swaps cannot race (#6060).
var packageregTracer = tracing.HandlerTracer()

// startQueryHandlerSpan wraps this family's HTTP handlers in stable spans and
// attaches low-cardinality route/capability attributes for operator triage.
//
// The implementation lives in package tracing so this family can start the same
// span without importing root package query, which it cannot do without an
// import cycle through root's compatibility aliases (#6060). The tracer name
// is unchanged, so emitted spans and the dashboards built on them are
// unaffected.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return tracing.StartHandlerSpanWith(packageregTracer, r, spanName, route, capability)
}

// writeRegistryReadFailure answers a failed graph, correlation, or aggregate
// read. A stale or timed-out PostgreSQL reader, a graph outage, or a graph
// deadline gets its shared verdict from querycontract.WriteGraphReadError
// (503 with Retry-After, or 504). Anything else answers message through
// tracing.WriteServerFailure: 500 with err recorded on the request span, or
// 499 with only the client-cancel event when the caller canceled the request.
// err text never reaches the client (#7674).
func writeRegistryReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}
