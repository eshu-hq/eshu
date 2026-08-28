// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationshiptools defines MCP registrations and dependency-neutral
// route selection for code-relationship stories, code-relationship analysis,
// and bounded relationship-edge listing.
//
// CodeTools, Tool, and AnalyzeCodeRelationshipsSchema return fresh copies of
// their canonical definitions or schema. CodeRoute and EdgeRoute convert
// decoded arguments into routecontract.Request values without executing them.
// The selectors also decide whether a tool belongs to this family. The parent
// mcp package owns global fanout order, root route adapters, dispatch,
// authorization, transport, timeouts, response budgets, response envelopes,
// and telemetry. The query package owns validation, graph reads, bounds, and
// result shaping. This package runs no query and must keep tool names,
// descriptions, definition order, input schemas, and selected request values
// stable.
package relationshiptools
