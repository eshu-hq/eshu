// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deadcodetools defines pure route selection for the MCP dead-code
// family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order (find_dead_code stays in
// the root codebase group in tools_codebase.go, investigate_dead_code in
// tools_dead_code.go, and find_cross_repo_dead_code in
// tools_cross_repo_dead_code.go), global route fanout, the private adapter,
// HTTP dispatch, authorization, timeouts, response budgets, envelopes,
// summaries, and telemetry. The query package owns the bounded reads behind
// the three POST /api/v0/code/dead-code paths. This package runs no query and
// must keep every tool name, request path, and body key stable.
//
// The three tools — find_dead_code, investigate_dead_code, and
// find_cross_repo_dead_code — share the exclude_decorated_with vocabulary
// and the limit default 100, the same value the handlers substitute for a
// nonpositive limit before clamping anything above 500. repo_id and (for
// investigate) language travel as strings even when empty; only the
// cross-repo route rejects a blank repo_id, while the scan and investigate
// routes widen to every repository the caller's scope grants.
//
// The two list arguments deliberately keep opposite absent shapes on the
// wire: exclude_decorated_with is a nil []any (JSON null) when absent or
// malformed and a non-nil empty []any (JSON []) when the caller sent an
// empty list, while consumer_repo_ids is always a non-nil []string (JSON []
// at minimum) whose non-string and empty members are dropped. Both shapes
// are inherited from the root helpers the switch arms used before the
// extraction.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "25" becomes the default rather than
// an error. Wrong-typed strings read as empty.
package deadcodetools
