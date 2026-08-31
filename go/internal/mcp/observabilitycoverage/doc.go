// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package observabilitycoveragetools defines pure route selection for the MCP
// observability-coverage family.
//
// Route decides whether this package owns a tool and maps decoded arguments to
// a dependency-neutral internal request without executing it. The parent mcp
// package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded read
// behind the path, which reports whether a monitored cloud resource or service
// has alarm, dashboard, log, or trace coverage. This package runs no query and
// must keep the tool name, request path, and query keys stable.
//
// The listing carries twelve query keys, the widest set the repository router
// selects: a keyset cursor, a limit defaulting to 50, and ten filters spanning
// scope, provider, coverage signal and status, observability object, source and
// resource class, outcome, and both target anchors. The handler reads each by
// name and has no catch-all. Dropping one is not uniformly silent: limit is
// required and a scope anchor is required, so losing either 400s the request,
// while losing coverage_status, source_class, resource_class, or outcome
// silently widens the page to rows the caller filtered out, and losing
// after_correlation_id silently breaks keyset paging.
package observabilitycoveragetools
