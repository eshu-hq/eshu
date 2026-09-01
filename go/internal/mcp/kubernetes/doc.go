// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kubernetestools defines pure route selection for the MCP
// Kubernetes-correlation family.
//
// Route decides whether this package owns a tool and maps decoded arguments to
// a dependency-neutral internal request without executing it. The parent mcp
// package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded read
// behind the path, which lists the reducer's Kubernetes workload ownership and
// drift correlations -- exact, derived, ambiguous, unresolved, stale, and
// rejected outcomes -- anchored by cluster, workload object, namespace, image
// reference, source digest, or scope. This package runs no query and must keep
// the tool name, request path, and query keys stable.
//
// The listing carries ten query keys: six anchors (scope_id, cluster_id,
// workload_object_id, namespace, image_ref, and source_digest), of which the
// handler requires at least one; outcome and drift_kind, optional equality
// filters; after_correlation_id, the keyset cursor a truncated page hands
// back; and limit, which defaults to 50 here and which the handler requires
// and bounds to 1..200 -- a limit of 0, -1, or 500 is a 400, not a clamp.
// Dropping one is not uniformly loud: losing limit 400s every request, losing
// an anchor 400s only the caller whose sole anchor it was and silently widens
// everyone else's page, losing outcome or drift_kind returns 200 over every
// outcome or drift kind, and losing after_correlation_id returns 200 from the
// first page again.
//
// limit honours only a Go number. The string "25" falls back to 50, so a
// client that stringifies the number gets a 50-row page rather than an error.
package kubernetestools
