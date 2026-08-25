// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationshiptools defines the MCP registration for listing bounded
// relationship edges.
//
// Tool returns a fresh copy of the canonical definition. The parent mcp
// package owns its global position, route resolution, dispatch, authorization,
// response envelopes, and telemetry. The query package owns graph reads and
// result shaping. This package runs no query and must keep the tool name,
// description, and input schema stable.
package relationshiptools
