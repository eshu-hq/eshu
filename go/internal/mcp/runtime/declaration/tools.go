// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimedeclarationtools

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// ToolName is the MCP tool this family owns.
const ToolName = "list_runtime_version_declarations"

// Tools returns the runtime declaration tool registration.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{listTool()}
}

func listTool() toolcontract.ToolDefinition {
	return toolcontract.ToolDefinition{
		Name: ToolName,
		Description: "List the runtime versions repositories declare (pin files such as .nvmrc, .tool-versions and rust-toolchain, package.json engines, volta and packageManager, and the go.mod go and toolchain directives), so a security question such as which repositories declare a Node major below 20 has one answer. " +
			"Send runtime and major_below (or version_below such as 20.10) to compare; a comparison needs runtime. " +
			"The comparison returns only declarations it can place (exact and partial specs). Range, alias (lts/*, node, system) and unresolved specs are never guessed: they are excluded and named in partial_reasons, with their counts in excluded_from_comparison. Read partial_reasons before concluding a fleet is clean. " +
			"There is no effective-version view: every declaration is returned with its scope_dir (. at the repository root), so a root .nvmrc and a services/web/.nvmrc are separate rows. " +
			"Rows are ordered by repo_id, relative_path, line_number, runtime and order_index. When truncated is true, pass next_cursor as cursor to continue. " +
			"Use get_repo_context for one repository's declared-runtime summary.",
		// The object stays open and length caps stay out of the schema: the
		// router validator implements neither bool additionalProperties nor
		// maxLength, and the HTTP handler enforces both (DisallowUnknownFields
		// plus the maxFilterLength/maxRepoIDLength/maxRepoIDs caps in
		// go/internal/query/runtime/declaration/request.go). Sibling tools
		// follow the same descriptions-plus-handler-validation split.
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"runtime": map[string]any{
					"type":        "string",
					"description": "Exact runtime name, such as node, python, go or rust. At most 128 characters. Required with major_below or version_below.",
				},
				"major_below": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"description": "Return declarations whose resolved major is below this value (major_below 20 finds Node 18 and 16). Requires runtime. Not with version_below.",
				},
				"version_below": map[string]any{
					"type":        "string",
					"pattern":     "^[0-9]{1,9}(\\.[0-9]{1,9}){0,2}$",
					"description": "Return declarations below major, major.minor or major.minor.patch, such as 20.10. A partial spec that is too imprecise for the bound (20 against 20.10) is excluded and named in partial_reasons. Requires runtime. Not with major_below.",
				},
				"source_kind": map[string]any{
					"type":        "string",
					"description": "Exact declaring source: nvmrc, tool_versions, rust_toolchain, package_json_engines, package_json_volta, package_json_package_manager, go_mod_go or go_mod_toolchain. At most 128 characters.",
				},
				"repo_ids": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"maxItems":    100,
					"description": "Restrict to these repository ids (at most 100, each at most 512 characters).",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum declarations to return.",
					"default":     50,
					"minimum":     1,
					"maximum":     200,
				},
				"cursor": map[string]any{
					"type":        "string",
					"description": "next_cursor from a prior truncated page.",
				},
			},
		},
	}
}
