// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package asktools defines MCP registration and pure route selection for Ask
// Eshu's natural-language answer tool.
//
// Tools returns a fresh copy of the canonical definition. Route maps decoded
// arguments to a dependency-neutral internal request without executing it. The
// parent mcp package owns the global position, route fanout, private adapter,
// dispatch, authorization, response envelopes, and telemetry. The query
// package owns default-off enforcement and answer execution. This package runs
// no query and must keep the Ask name, description, schema, and request shape
// stable.
package asktools
