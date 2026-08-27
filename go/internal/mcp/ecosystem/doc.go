// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ecosystemtools defines the MCP registrations for ecosystem,
// repository-context, infrastructure-impact, and change-planning reads.
//
// Tools returns fresh copies of the 23 canonical definitions. The parent mcp
// package owns their client-visible positions, split route resolution,
// dispatch, authorization, query execution, response envelopes, transport, and
// telemetry. This package runs no query and must keep the registered names,
// descriptions, input schemas, and local order stable.
package ecosystemtools
