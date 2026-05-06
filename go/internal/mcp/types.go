package mcp

// ToolDefinition describes one MCP tool exposed to clients.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// ReadOnlyTools returns all read-only MCP tool definitions.
func ReadOnlyTools() []ToolDefinition {
	tools := make([]ToolDefinition, 0, 40)
	tools = append(tools, codebaseTools()...)
	tools = append(tools, ecosystemTools()...)
	tools = append(tools, contextTools()...)
	tools = append(tools, contentTools()...)
	tools = append(tools, runtimeTools()...)
	return tools
}
