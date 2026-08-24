// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package asktools defines the MCP registration for Ask Eshu's natural-language
// answer tool.
//
// Tools returns a fresh copy of the canonical definition. The parent mcp
// package owns its global position, route resolution, dispatch, authorization,
// response envelopes, and telemetry. The query package owns default-off
// enforcement and answer execution. This package runs no query and must keep
// the ask name, description, and input schema stable.
package asktools
