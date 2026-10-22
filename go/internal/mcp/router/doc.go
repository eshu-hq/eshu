// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package router defines the shared contract of the opt-in MCP router surface:
// the tool-surface selector, the find_tool and call_tool definitions, and the
// Ranker and Dispatcher seams that the ranker, dispatch, and surface-switch
// lanes build against.
//
// The package holds types and static definitions only. It runs no ranking, no
// argument validation, and no dispatch. The parent mcp package injects a
// Dispatcher and intercepts find_tool and call_tool before dispatchTool, so
// this package must never import the mcp root.
package router
