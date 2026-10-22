// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

// coreToolNames lists the tools SurfaceCoreRouter advertises beside find_tool
// and call_tool. It is empty in the scaffold; the core-set lane derives it
// from hosted call counts.
var coreToolNames = []string{}

// CoreToolNames returns a fresh copy of the tool names SurfaceCoreRouter
// advertises beside find_tool and call_tool. Callers may modify the result.
func CoreToolNames() []string {
	return append([]string(nil), coreToolNames...)
}
