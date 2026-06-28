// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

// relationshipEdgesRoute maps a list_relationship_edges call to the bounded
// POST /api/v0/relationships/edges endpoint. It validates source_tool against
// the canonical vocabulary before forwarding; an unknown token is rejected with
// a clear error so the agent gets immediate feedback rather than a silent empty
// result. An empty source_tool passes no filter (the API returns all edges for
// the verb).
func relationshipEdgesRoute(toolName string, args map[string]any) (*route, bool, error) {
	if toolName != "list_relationship_edges" {
		return nil, false, nil
	}

	tool := strings.ToLower(strings.TrimSpace(str(args, "source_tool")))
	if tool != "" && !sourcetool.IsValid(tool) {
		return nil, true, fmt.Errorf("unknown source_tool %q: must be one of the canonical vocabulary values", tool)
	}

	body := map[string]any{
		"verb":  str(args, "verb"),
		"limit": intOr(args, "limit", 50),
	}
	if tool != "" {
		body["source_tool"] = tool
	}

	return &route{method: "POST", path: "/api/v0/relationships/edges", body: body}, true, nil
}
