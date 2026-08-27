// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package freshnesstools defines the MCP registrations for generation,
// repository, and service freshness reads.
//
// Tools returns fresh copies of the four canonical definitions. The parent mcp
// package owns their client-visible order, route resolution, dispatch,
// authorization, response envelopes, and telemetry. This package runs no query
// and must keep the registered names, descriptions, and input schemas stable.
package freshnesstools
