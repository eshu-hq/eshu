// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicetools defines the MCP registrations for service catalog,
// service context, and service intelligence reads.
//
// CatalogTools, ContextTools, and IntelligenceTools return fresh copies of the
// five canonical definitions. The parent mcp package owns their client-visible
// positions, route resolution, dispatch, authorization, query execution,
// response envelopes, transport, and telemetry. This package runs no query and
// must keep the registered names, descriptions, and input schemas stable.
package servicetools
