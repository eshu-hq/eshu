// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// repositoryFilesTool defines the bounded repository file-listing tool. It
// proxies GET /api/v0/repositories/{repo_id}/tree, which reconstructs a
// directory tree from the content-index file store. The optional language
// filter restricts returned file entries to those whose stored language
// matches; it uses the same alias expansion as the by-language inventory
// endpoint (e.g. typescript also matches tsx). An unrecognised language
// token yields an empty listing rather than an error, mirroring the API.
func repositoryFilesTool() ToolDefinition {
	return ToolDefinition{
		Name: "list_repository_files",
		Description: "List indexed files for a repository, reconstructed from the content-index file store. " +
			"Returns a bounded directory tree with file names, paths, types (file/dir), " +
			"sizes (line counts), and languages. " +
			"language filters entries to a single language family (e.g. go, typescript, python, terraform). " +
			"path scopes the listing to a subdirectory; recursive expands the full subtree. " +
			"truncated is set when the repository has more than 50 000 indexed files.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_id": map[string]any{
					"type":        "string",
					"description": "Repository selector: canonical ID, name, repo slug, or indexed path.",
				},
				"language": map[string]any{
					"type":        "string",
					"description": "Optional language filter. Restricts file entries to those whose stored language matches this value. Uses the same alias expansion as the by-language inventory (e.g. typescript also matches tsx). An unrecognised token yields an empty listing.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional repository-relative subdirectory path to scope the listing.",
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "When true, expands the full subtree under path instead of a single directory level.",
					"default":     false,
				},
				"ref": map[string]any{
					"type":        "string",
					"description": "Optional commit SHA or ref to validate the indexed ref against. An unknown ref is rejected.",
				},
			},
			"required": []string{"repo_id"},
		},
	}
}
