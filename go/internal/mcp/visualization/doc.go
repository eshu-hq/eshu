// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package visualizationtools defines MCP registration and pure route selection
// for deriving a visualization packet from an already-authorized response.
//
// Tools returns a fresh copy of the canonical definition. Route maps decoded
// arguments to a dependency-neutral internal request without executing it. The
// parent mcp package owns the global position, route fanout, private adapter,
// dispatch, authorization, response envelopes, summaries, and telemetry. The
// query package owns packet derivation and validation. This package runs no
// query and must keep the tool name, schema, and request shape stable.
package visualizationtools
