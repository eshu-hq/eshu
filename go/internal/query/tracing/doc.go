// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tracing owns the per-route span that query HTTP reads emit, and the
// shared answer a query route gives when a server-side read fails.
//
// It exists so a handler-family subpackage under go/internal/query can start the
// same span without importing the root query package, which it cannot do
// without an import cycle through root's compatibility aliases (#6060). The
// instrumentation-scope name stays "eshu/go/internal/query" regardless of where
// the code sits, because saved span queries and dashboards match on that name.
//
// Callers pass their own tracer to StartHandlerSpanWith rather than the package
// reading a shared one. That keeps a test's recording provider private to the
// package that installed it: a swap in one family cannot change what another
// family records, and two such swaps cannot race.
//
// WriteServerFailure and ServerFailureEnvelope answer a failed read with a
// fixed message the caller supplies, never the backend error text (#7626). A
// server fault (500, or 504 for a route's own read budget) records the error
// on the span and sets its status to Error. A client cancel, meaning the error
// wraps context.Canceled and the request context itself is canceled, answers
// querycontract.StatusClientClosedRequest (499), leaves the span status unset,
// and adds only the ClientCanceledEvent span event. ClientCanceled is that
// predicate, exported so a route that logs a failure classifies a cancel the
// same way the helpers choose 499.
package tracing
