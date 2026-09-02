// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impacttools defines pure route selection for the MCP
// impact-analysis family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The ecosystem child owns eight of the
// nine advertised definitions; trace_exposure_path is registered at the
// parent's root. The query package owns the bounded reads behind the nine
// /api/v0/impact/ paths. This package runs no query and must keep every tool
// name, request path, and body key stable.
//
// Eight of the nine builders select their bodies key by key, with
// dispatcher-side defaults the tests pin: limit 25 for
// investigate_deployment_config, investigate_contract_impact, and
// investigate_change_surface; limit 50 for find_blast_radius,
// find_change_surface, and trace_resource_to_code; max_depth 4 for
// investigate_change_surface, 8 for trace_resource_to_code, and 5 for
// trace_exposure_path; and direct_only true for trace_deployment_chain.
// trace_deployment_chain forwards max_depth 0 when omitted so the handler
// applies its own operator-safe default (boundedTraceEnrichmentLimit(0) =
// 25); the handler clamps max_depth into [0, 1000] rather than rejecting it.
// explain_dependency_path is the exception: it forwards the caller's decoded
// argument map itself as the body, selecting, defaulting, and coercing
// nothing.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "25" becomes the default rather than
// an error. Wrong-typed strings read as empty, and a non-bool direct_only
// falls back to true.
package impacttools
