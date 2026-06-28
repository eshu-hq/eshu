// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "net/http"

func codeFlowRoute(toolName string, args map[string]any) (*route, bool) {
	paths := map[string]string{
		"dispatch_taint_path":   "/api/v0/code/flow/taint-path",
		"dispatch_reaching_def": "/api/v0/code/flow/reaching-def",
		"dispatch_cfg_summary":  "/api/v0/code/flow/cfg-summary",
		"dispatch_pdg_summary":  "/api/v0/code/flow/pdg-summary",
	}
	path, ok := paths[toolName]
	if !ok {
		return nil, false
	}
	return &route{
		method: http.MethodPost,
		path:   path,
		body: map[string]any{
			"repo_id":   str(args, "repo_id"),
			"language":  str(args, "language"),
			"symbol":    str(args, "symbol"),
			"file_path": str(args, "file_path"),
			"line":      intOr(args, "line", 0),
			"limit":     intOr(args, "limit", 25),
		},
	}, true
}
