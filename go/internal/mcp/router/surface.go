// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"fmt"
	"strings"
)

// Surface selects which tools the MCP server advertises in tools/list. It only
// changes advertisement: a direct tools/call to any registered tool keeps
// working under every surface.
type Surface string

const (
	// SurfaceFull advertises every registered tool, exactly as before the
	// router existed. It is the default.
	SurfaceFull Surface = "full"
	// SurfaceRouter advertises only find_tool and call_tool.
	SurfaceRouter Surface = "router"
	// SurfaceCoreRouter advertises the core high-traffic tool set plus
	// find_tool and call_tool.
	SurfaceCoreRouter Surface = "core_router"
)

// ParseSurface converts a configuration value into a Surface. An empty string
// selects SurfaceFull so an unset ESHU_MCP_TOOL_SURFACE keeps today's behavior.
// Matching is case-insensitive; any other value, including one with
// surrounding whitespace, is an error so a typo cannot silently fall back.
func ParseSurface(raw string) (Surface, error) {
	switch Surface(strings.ToLower(raw)) {
	case "", SurfaceFull:
		return SurfaceFull, nil
	case SurfaceRouter:
		return SurfaceRouter, nil
	case SurfaceCoreRouter:
		return SurfaceCoreRouter, nil
	default:
		return "", fmt.Errorf("unknown MCP tool surface %q (want full, router, or core_router)", raw)
	}
}
