// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package investigationtools defines the MCP registrations for investigation
// workflow discovery and evidence-packet exports.
//
// WorkflowTools and PacketTools return fresh copies of the five canonical
// definitions. The parent mcp package owns their client-visible order, route
// resolution, dispatch, authorization, query execution, response envelopes,
// transport, and telemetry. This package runs no query and must keep the
// registered names, descriptions, and input schemas stable.
package investigationtools
