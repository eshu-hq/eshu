// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

// coreToolNames lists the tools SurfaceCoreRouter advertises beside find_tool
// and call_tool. The list is the top 10 tools by share of measured MCP calls
// on a QA deployment, in descending share order (#8759). The measurement, the
// shares, and the deviation checks are in
// docs/internal/design/8759-mcp-core-router-tool-set.md; change this list only
// with a new measurement and a matching change to that record and to
// measuredCoreToolNames in go/internal/mcp/core_tool_set_test.go.
//
// Reason per tool (share of traffic, percent):
//   - list_codeowners_ownership (29.6): top tool; who owns a path. The QA
//     sample may be test-driver heavy.
//   - search_file_content (19.0): text search across indexed file bodies.
//   - get_file_lines (12.4): read a line range after a search hit.
//   - get_repository_freshness (6.5): is this repository's index current.
//   - get_file_content (3.5): read a whole file by repository and path.
//   - list_repository_files (3.4): list a repository's indexed files.
//   - find_code (2.6): symbol and code lookup by name.
//   - list_repositories_by_language (2.6): which repositories use a language.
//   - find_infra_resources (2.4): infrastructure resource search.
//   - investigate_code_topic (2.4): topic-level code investigation.
var coreToolNames = []string{
	"list_codeowners_ownership",
	"search_file_content",
	"get_file_lines",
	"get_repository_freshness",
	"get_file_content",
	"list_repository_files",
	"find_code",
	"list_repositories_by_language",
	"find_infra_resources",
	"investigate_code_topic",
}

// CoreToolNames returns a fresh copy of the tool names SurfaceCoreRouter
// advertises beside find_tool and call_tool. Callers may modify the result.
func CoreToolNames() []string {
	return append([]string(nil), coreToolNames...)
}
