// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

// coreToolNames lists the tools SurfaceCoreRouter advertises beside find_tool
// and call_tool. The list is the top 10 tools by mean per-day share of MCP
// calls over a 21-day QA window (2026-10-02 to 2026-10-22), with one
// canonical-family swap (#8759). The window, the rule, the scores, and the
// deviations are in docs/internal/design/8759-mcp-core-router-tool-set.md;
// change this list only with a new measurement and a matching change to that
// record and to measuredCoreToolNames in go/internal/mcp/core_tool_set_test.go.
// The traffic is concentrated on a few days that look like test-driver runs,
// so the list is not a measure of assistant usage (#8760 measures that).
//
// Reason per tool (per-day score, percent):
//   - search_file_content (26.3): text search across indexed file bodies.
//   - get_service_context (10.4): one-call service dossier.
//   - get_file_content (5.5): read a whole file by repository and path.
//   - find_cross_repo_dead_code (5.0): dead-code candidates across repositories.
//   - search_semantic_context (4.9): semantic search over indexed context.
//   - investigate_dead_code (3.5): canonical dead-code investigation.
//   - plan_developer_change (3.3): plan a change with its surface and impact.
//   - get_file_lines (2.5): read a line range after a search hit.
//   - analyze_pre_change_impact (2.5): impact of a change before it is made.
//   - get_repo_story (0.1): canonical repository-overview tool. It replaces
//     get_repo_context (2.8) by the record's canonical-swap rule.
var coreToolNames = []string{
	"search_file_content",
	"get_service_context",
	"get_file_content",
	"find_cross_repo_dead_code",
	"search_semantic_context",
	"investigate_dead_code",
	"plan_developer_change",
	"get_file_lines",
	"analyze_pre_change_impact",
	"get_repo_story",
}

// CoreToolNames returns a fresh copy of the tool names SurfaceCoreRouter
// advertises beside find_tool and call_tool. Callers may modify the result.
func CoreToolNames() []string {
	return append([]string(nil), coreToolNames...)
}
