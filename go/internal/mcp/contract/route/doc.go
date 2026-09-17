// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package routecontract defines dependency-neutral values for MCP route
// selection.
//
// Arguments preserves the root dispatcher's accepted argument coercions.
// Request carries the internal HTTP method, path, body, and query selected by a
// domain router. Family packages own family membership and route-selection
// policy; this package does not own tool names, global route fanout, adapters, HTTP dispatch,
// authorization, timeouts, response budgets, envelopes, transport behavior,
// telemetry, or query execution.
package routecontract
