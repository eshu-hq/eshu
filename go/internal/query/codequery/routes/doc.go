// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package routes implements route-to-caller tracing for the code-family
// queries: the route request and its validation, endpoint and HANDLES_ROUTE
// handler resolution, the bounded CALLS traversal around the handler, and
// the workload/repository impact assembly. It split out of package
// codequery (#6060 naming follow-up) so the trace can be read, tested, and
// changed without pulling in the rest of the code surface, and so it can
// one day move into its own repo.
//
// The split keeps four *CodeHandler graph-read methods in
// codequery/route_handlers.go -- routeToCallerEndpointRows,
// routeToCallerHandlerLabel, routeToCallerHandlerRows, and
// routeToCallerImpactRows carry queryplan source_sha256 pins in
// grandfathered_non_hot.go, which are never re-frozen -- and calls this
// leaf through same-named forwarders. The HTTP handler, the impact
// assembler, and the request/route aliases stay in codequery for the same
// reason. Every Cypher literal below moved verbatim; only the surrounding
// Go changed (explicit graph, context, and grant parameters instead of the
// handler receiver).
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses (chain, querycontract, rows) but NEVER
// package codequery itself and NEVER root package query -- both would
// create an import cycle (codequery calls this leaf, and root aliases
// codequery).
package routes
