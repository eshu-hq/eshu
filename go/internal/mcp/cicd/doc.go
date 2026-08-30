// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicdtools defines pure route selection for the MCP CI/CD
// run-correlation family.
//
// Route decides which of the three CI/CD run-correlation tools this package
// owns and maps decoded arguments to a dependency-neutral internal request
// without executing it. The parent mcp package owns tool registration and its
// order, global route fanout, the private adapter, HTTP dispatch,
// authorization, timeouts, response budgets, envelopes, summaries, and
// telemetry. The query package owns the bounded reads behind these paths. This
// package runs no query and must keep the tool names, request paths, and query
// keys stable, including the provider_run_id fallback to run_id that the
// listing route has always applied.
package cicdtools
