// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeinteltools defines pure route selection and the eight MCP
// code-intelligence tool definitions.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns the root registration splice and client-visible order
// (this package owns all eight definitions; the root splices them at their
// long-standing positions), global route fanout, the
// private codeIntelRoute adapter, HTTP dispatch, authorization, timeouts,
// response budgets, envelopes, summaries, and telemetry. The query package
// owns the bounded reads behind each POST /api/v0/code/... path. This
// package runs no query and must keep every tool name, request path, and
// body key stable.
//
// The eight tools — find_code, find_symbol, inspect_code_inventory,
// inspect_call_graph_metrics, trace_route_callers, investigate_code_topic,
// execute_language_query, and find_function_call_chain — each build a
// distinct body shape; nothing here is shared across tools the way the
// code-flow family shares one six-key body. Every string field travels even
// when empty, so a handler sees an explicit blank filter rather than a
// missing field.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "25" becomes the default rather than
// an error.
package codeinteltools
