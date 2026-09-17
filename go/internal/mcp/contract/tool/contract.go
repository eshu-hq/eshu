// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package toolcontract

// ToolDefinition describes one MCP tool exposed to clients.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}
