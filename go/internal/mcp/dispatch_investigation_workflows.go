// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

func investigationWorkflowRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	switch toolName {
	case "list_investigation_workflows":
		query := map[string]string{
			"limit":  intString(args, "limit", 20),
			"offset": intString(args, "offset", 0),
		}
		if view := str(args, "view"); view != "" {
			query["view"] = view
		}
		return &routecontract.Request{Method: "GET", Path: "/api/v0/investigation-workflows", Query: query}, true
	case "resolve_investigation_workflow":
		return &routecontract.Request{
			Method: "POST",
			Path:   "/api/v0/investigation-workflows/resolve",
			Body: map[string]any{
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
