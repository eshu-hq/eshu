// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codedivergencetools defines pure route selection and the tool
// definitions for the MCP code-divergence family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns the root registration wrapper and client-visible order,
// global route fanout, the private adapter, HTTP dispatch, authorization,
// timeouts, response budgets, envelopes, summaries, and telemetry. The query
// package owns the bounded reads behind
// POST /api/v0/code/divergence/findings and
// POST /api/v0/code/divergence/investigate. This
// package runs no query and must keep every tool name, request path, and
// body key stable.
//
// find_code_divergence defaults limit to 25, the same value the handler
// substitutes for a nonpositive limit before clamping anything above 100
// down to 100, so the dispatcher's default is indistinguishable from an
// omitted limit. A negative offset rejects with HTTP 400, as does anything
// above 10000. kind travels
// blank for the all-families read; investigate_code_divergence requires an
// explicit kind (exact, renamed, drifted, wrapper_bypass, or
// convention_outlier, short or qualified) because a fingerprint is only
// unique within its family.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "25" becomes the default rather than
// an error. Wrong-typed strings read as empty.
package codedivergencetools
