// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package visualizationtools defines the MCP registration for deriving a
// visualization packet from an already-authorized response.
//
// Tools returns a fresh copy of the one canonical definition. The parent mcp
// package owns its global position, route resolution, dispatch, authorization,
// response envelopes, and telemetry. This package runs no query and must keep
// the derive_visualization_packet name, description, and input schema stable.
package visualizationtools
