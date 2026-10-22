// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// SpanMCPRouterFind wraps one MCP find_tool request: argument validation,
// the per-call credential filter, ranking, and the playbook match (#8757).
const SpanMCPRouterFind = "mcp.router.find"

// SpanMCPRouterCall wraps one MCP call_tool request: target refusal and
// lookup, argument validation against the target's input schema, the
// credential check, and the dispatch of the target tool (#8757).
const SpanMCPRouterCall = "mcp.router.call"
