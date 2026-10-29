// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apicontract

// ToolDefinition describes one MCP tool exposed to clients. It mirrors the
// dependency-neutral toolcontract.ToolDefinition the core MCP server builds
// its tools/list responses from, so a thin client can decode that response
// without importing the core. Tool ordering, routing, dispatch, and
// authorization stay server-side; only the advertised shape is published.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}
