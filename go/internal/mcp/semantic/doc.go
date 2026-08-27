// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semantictools defines the MCP registrations for semantic evidence
// and semantic context search reads.
//
// EvidenceTools and SearchTools return fresh copies of the three canonical
// definitions. The parent mcp package owns their client-visible order, route
// resolution, dispatch, authorization, query execution, response envelopes,
// transport, and telemetry. This package runs no query and must keep the
// registered names, descriptions, and input schemas stable.
package semantictools
