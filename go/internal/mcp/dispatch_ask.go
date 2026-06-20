package mcp

// askRoute maps the "ask" tool to POST /api/v0/ask.
//
// The endpoint is default-off: when ESHU_ASK_ENABLED is unset or the
// agent_reasoning provider profile is missing, the handler returns
// 503 with state "unavailable" rather than running the engine. The
// MCP dispatch surface treats that as a non-error envelope response so
// callers see a clean tool result rather than a transport error.
func askRoute(toolName string, args map[string]any) (*route, bool) {
	if toolName != "ask" {
		return nil, false
	}
	return &route{
		method: "POST",
		path:   "/api/v0/ask",
		body: map[string]any{
			"question": str(args, "question"),
			"format":   str(args, "format"),
		},
	}, true
}
