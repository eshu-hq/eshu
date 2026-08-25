// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package playbooktools defines the MCP registrations for query-playbook
// catalog tools.
//
// Tools returns fresh copies of the canonical definitions. The parent mcp
// package owns their global positions, route resolution, dispatch,
// authorization, response envelopes, and telemetry. This package runs no
// query and must keep both names, descriptions, schemas, and local order
// stable.
package playbooktools
