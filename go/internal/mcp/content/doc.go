// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contenttools defines pure route selection for the MCP content
// family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order (all five tools —
// get_file_content, get_file_lines, build_evidence_citation_packet,
// search_file_content, and search_entity_content — stay registered together
// in tools_content.go, alongside get_entity_content, whose own route
// selection lives in entityresolution instead), global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded
// reads behind POST /api/v0/content/files/read,
// POST /api/v0/content/files/lines, POST /api/v0/evidence/citations,
// POST /api/v0/content/files/search, and POST /api/v0/content/entities/search.
// This package runs no query and must keep every tool name, request method,
// path, and body key stable.
//
// get_file_content builds a fresh body carrying repo_id and relative_path,
// each an explicit empty string when absent. get_file_lines is the one
// exception in this family: its body is the caller's decoded arguments
// forwarded verbatim, not a freshly built map, so the handler alone owns
// start_line, end_line, repo_id, and relative_path validation; this
// preserves the root switch arm's aliasing rather than copying it into a new
// map.
//
// build_evidence_citation_packet forwards subject and handles unchanged
// (nil when absent) and defaults limit to 10, the same value the handler
// substitutes for an absent or nonpositive limit before capping it at the
// advertised maximum of 50; the advertised schema itself caps the incoming
// handles array at 500.
//
// search_file_content and search_entity_content share contentSearchBody,
// the selection this pair depended on before the extraction: query prefers
// the query argument and falls back to pattern when query is blank; the
// repo scope collapses to a single repo_id key when zero or one repo
// selector is supplied and switches to repo_ids only when more than one is
// supplied, because the query handlers accept only one of the two shapes
// per call. limit defaults to 10 here, offset to 0. The 10 is this
// dispatch selector's own choice, matching the advertised schema default in
// tools_content.go — it is NOT the handler's default: an absent or
// nonpositive limit reaching the handler independently substitutes 50
// (contentSearchDefaultLimit in query's content_handler.go) before clamping
// anything above 200 (contentSearchMaxLimit) down to 200, so a caller who
// bypasses the advertised schema default still gets a bounded search.
// Moving both tools into this package keeps the shared helper with the
// family that owns it, per the entityresolution and codeintel package
// docs, which explain why search_entity_content stayed out of their
// families instead.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "10" becomes the default rather than
// an error. Wrong-typed strings read as empty.
package contenttools
