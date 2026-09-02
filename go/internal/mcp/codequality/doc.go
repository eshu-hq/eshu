// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codequalitytools defines pure route selection for the MCP
// complexity/quality family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order
// (calculate_cyclomatic_complexity and find_most_complex_functions stay in
// the root codebase group in tools_codebase.go, and inspect_code_quality in
// tools_code_quality.go), global route fanout, the private adapter, HTTP
// dispatch, authorization, timeouts, response budgets, envelopes, summaries,
// and telemetry. The query package owns the bounded reads behind
// POST /api/v0/code/complexity and POST /api/v0/code/quality/inspect. This
// package runs no query and must keep every tool name, request path, and
// body key stable.
//
// The two complexity tools share one path and one handler, which branches on
// the selectors: a non-empty entity_id or function_name answers a
// single-function lookup, and blank selectors answer the most-complex list.
// calculate_cyclomatic_complexity carries entity_id only when the caller
// supplied a non-empty string — the key's absence is the pinned wire shape —
// and sends no limit key at all, so a blank-selector call reaches the
// handler's own list default of 10.
//
// find_most_complex_functions and inspect_code_quality default limit to 10,
// the same value both handlers substitute for a nonpositive limit before
// clamping anything above 100 down to 100, so the dispatcher's default is
// indistinguishable from an omitted limit and no limit value can 400.
// inspect_code_quality's offset bounds act in opposite directions: the
// handler floors a negative to 0 but rejects anything above 10000 with HTTP
// 400. The three min_* thresholds travel as 0 when omitted so the handler
// resolves its own check-specific defaults; a blank check resolves to
// refactoring_candidates there, while an unsupported non-blank check rejects
// with HTTP 400.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "25" becomes the default rather than
// an error. Wrong-typed strings read as empty.
package codequalitytools
