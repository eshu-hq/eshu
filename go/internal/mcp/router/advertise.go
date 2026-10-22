// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

// Advertise returns the tool definitions tools/list shows for surface.
//
// tools is the registry after any query-profile filtering, so a tool hidden by
// the profile stays hidden in every mode:
//
//   - SurfaceFull returns tools unchanged, the same slice, so the response is
//     byte-identical to a server with no surface option.
//   - SurfaceRouter returns Tools() only.
//   - SurfaceCoreRouter returns the CoreToolNames tools in registration order,
//     then Tools().
//
// The non-full results are fresh slices. A surface value ParseSurface would
// reject is treated as SurfaceFull; callers validate configuration first.
func Advertise(tools []toolcontract.ToolDefinition, surface Surface) []toolcontract.ToolDefinition {
	switch surface {
	case SurfaceRouter:
		return Tools()
	case SurfaceCoreRouter:
		return advertiseCoreRouter(tools, CoreToolNames())
	default:
		return tools
	}
}

// advertiseCoreRouter keeps the tools named in core, in the order tools lists
// them, and appends the router pair. A core name that is not in tools (for
// example one the query profile hides) is skipped.
func advertiseCoreRouter(tools []toolcontract.ToolDefinition, core []string) []toolcontract.ToolDefinition {
	want := make(map[string]struct{}, len(core))
	for _, name := range core {
		want[name] = struct{}{}
	}
	pair := Tools()
	out := make([]toolcontract.ToolDefinition, 0, len(core)+len(pair))
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			out = append(out, tool)
		}
	}
	return append(out, pair...)
}
