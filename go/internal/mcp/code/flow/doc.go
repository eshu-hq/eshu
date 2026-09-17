// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeflowtools defines pure route selection for the MCP code-flow
// family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order (the four definitions stay
// at the parent's root in tools_code_flow.go), global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded
// reads behind the four POST /api/v0/code/flow/ paths. This package runs no
// query and must keep every tool name, request path, and body key stable.
//
// The four tools — dispatch_taint_path, dispatch_reaching_def,
// dispatch_cfg_summary, and dispatch_pdg_summary — share one six-key body:
// repo_id, language, symbol, and file_path travel as strings even when empty,
// line defaults to 0 (the handler's no-line-filter value, load-bearing for
// its symbol-ambiguity signal), and limit defaults to 25, the same value the
// handler substitutes for a nonpositive limit before clamping anything above
// 100. Only a blank repo_id can reject; every other selected value the
// handler normalizes rather than refuses.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "25" becomes the default rather than
// an error. Wrong-typed strings read as empty.
package codeflowtools
