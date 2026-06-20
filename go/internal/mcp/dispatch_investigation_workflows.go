package mcp

func investigationWorkflowRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "list_investigation_workflows":
		return &route{method: "GET", path: "/api/v0/investigation-workflows"}, true
	case "resolve_investigation_workflow":
		return &route{
			method: "POST",
			path:   "/api/v0/investigation-workflows/resolve",
			body: map[string]any{
				"workflow_id":      str(args, "workflow_id"),
				"inputs":           mapStringAny(args, "inputs"),
				"missing_evidence": stringValues(args, "missing_evidence"),
			},
		}, true
	default:
		return nil, false
	}
}

func stringValues(args map[string]any, key string) []string {
	raw := stringSlice(args, key)
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if ok && text != "" {
			values = append(values, text)
		}
	}
	return values
}
