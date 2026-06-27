// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func semanticSearchTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:        "search_semantic_context",
		Description: "Search curated Eshu context for a repository with explicit scope, limit, timeout, truth, and truncation metadata.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Repository id that bounds the searchable corpus.",
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Search query text.",
				},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []any{"keyword", "semantic", "hybrid"},
					"description": "Retrieval mode. Hybrid may report bm25 when no local embedder is configured.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     100,
					"description": "Explicit top-K result limit.",
				},
				"timeout_ms": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"description": "Server-side retrieval timeout in milliseconds.",
				},
				"service_id": map[string]any{
					"type":        "string",
					"description": "Optional smaller service anchor inside the repository corpus.",
				},
				"workload_id": map[string]any{
					"type":        "string",
					"description": "Optional smaller workload anchor inside the repository corpus.",
				},
				"environment": map[string]any{
					"type":        "string",
					"description": "Optional environment anchor inside the repository corpus.",
				},
				"source_kinds": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": []any{
							"code_entity",
							"repository_file",
							"runtime_summary",
							"semantic_context",
						},
					},
					"description": "Optional curated document source-kind filter.",
				},
				"languages": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"description": "Optional language filter. Documents are included only when their Labels array " +
						"contains language:<lang> for one of the listed values. " +
						"Use recognized parser-registry language values (e.g. go, python, typescript). " +
						"An empty array means no filter. Unknown values are rejected with HTTP 400.",
				},
				"rerank": map[string]any{
					"type": "boolean",
					"description": "Opt into graph-neighborhood reranking over the in-scope results. " +
						"When true the response reports the reranking state, per-result ranking basis, " +
						"and recommended next calls. Off by default.",
				},
			},
			"required": []any{"repo_id", "query", "mode", "limit", "timeout_ms"},
		},
	}}
}
