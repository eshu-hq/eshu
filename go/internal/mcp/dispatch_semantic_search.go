// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func semanticSearchRoute(toolName string, args map[string]any) (*route, bool) {
	if toolName != "search_semantic_context" {
		return nil, false
	}
	return &route{
		method: "POST",
		path:   "/api/v0/search/semantic",
		body: map[string]any{
			"repo_id":      str(args, "repo_id"),
			"query":        str(args, "query"),
			"mode":         str(args, "mode"),
			"limit":        intOr(args, "limit", 0),
			"timeout_ms":   intOr(args, "timeout_ms", 0),
			"service_id":   str(args, "service_id"),
			"workload_id":  str(args, "workload_id"),
			"environment":  str(args, "environment"),
			"source_kinds": stringSlice(args, "source_kinds"),
			"languages":    stringSlice(args, "languages"),
			"rerank":       boolOr(args, "rerank", false),
		},
	}, true
}
