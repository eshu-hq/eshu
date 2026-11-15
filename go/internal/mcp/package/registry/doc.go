// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packageregistrytools defines pure route selection for the MCP
// package-registry family.
//
// Route decides which of the six package-registry tools this package owns and
// maps decoded arguments to a dependency-neutral internal request without
// executing it. The parent mcp package owns tool registration and its order,
// global route fanout, the private adapter, HTTP dispatch, authorization,
// timeouts, response budgets, envelopes, summaries, and telemetry. The query
// package owns the bounded reads behind these paths. This package runs no
// query and must keep the tool names, request paths, and query keys stable.
package packageregistrytools
