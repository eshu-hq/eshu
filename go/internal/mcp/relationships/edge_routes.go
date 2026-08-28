// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshiptools

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

// EdgeRoute selects the internal HTTP request for a relationship-edge tool.
// It validates the optional source-tool filter but does not execute the
// request.
func EdgeRoute(toolName string, args routecontract.Arguments) (routecontract.Request, bool, error) {
	if toolName != "list_relationship_edges" {
		return routecontract.Request{}, false, nil
	}

	tool := strings.ToLower(strings.TrimSpace(args.String("source_tool")))
	if tool != "" && !sourcetool.IsValid(tool) {
		return routecontract.Request{}, true, fmt.Errorf("unknown source_tool %q: must be one of the canonical vocabulary values", tool)
	}

	body := map[string]any{
		"verb":  args.String("verb"),
		"limit": args.IntOr("limit", 50),
	}
	if tool != "" {
		body["source_tool"] = tool
	}

	return routecontract.Request{Method: "POST", Path: "/api/v0/relationships/edges", Body: body}, true, nil
}
