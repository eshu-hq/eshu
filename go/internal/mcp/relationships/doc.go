// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationshiptools defines MCP registrations for code-relationship
// stories, code-relationship analysis, and bounded relationship-edge listing.
//
// CodeTools and Tool return fresh copies of their canonical definitions. The
// parent mcp package owns their global positions, route resolution, dispatch,
// authorization, response envelopes, and telemetry. The query package owns
// graph reads and result shaping. This package runs no query and must keep tool
// names, descriptions, definition order, and input schemas stable.
package relationshiptools
