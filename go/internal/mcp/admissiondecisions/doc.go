// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admissiondecisionstools defines pure route selection for the MCP
// admission-decisions family.
//
// Route decides whether this package owns a tool and maps decoded arguments to
// a dependency-neutral internal request without executing it. The parent mcp
// package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded read
// behind the path, which lists the reducer's correlation admission decisions
// -- admitted, rejected, ambiguous, stale, missing-evidence, and
// permission-hidden candidates -- for one domain, scope, and generation. This
// package runs no query and must keep the tool name, request path, and query
// keys stable.
//
// The listing carries eight query keys: domain, scope_id, and generation_id,
// which the handler requires; anchor_kind and anchor_id, which it requires
// together or not at all; an optional state filter drawn from the handler's
// vocabulary; include_evidence, sent as an explicit "true" or "false"; and a
// limit defaulting to 50: the handler substitutes that default for any
// nonpositive value and caps anything above 200, so a limit of 0 or -1 serves
// 50 rows rather than one. Dropping one is not uniformly loud: losing any of
// the three required keys or one half of
// the anchor pair 400s the request, while losing state, include_evidence,
// limit, or both anchor halves returns 200 with a wider state set, no
// evidence rows, a 50-row page, or every anchor in scope.
//
// include_evidence honours only a Go bool. The strings "true" and "1" fall
// back to false, so a client that stringifies the flag gets a 200 with no
// evidence rows rather than an error.
package admissiondecisionstools
