// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package routecontract defines dependency-neutral values for MCP route
// selection.
//
// Arguments preserves the root dispatcher's accepted argument coercions.
// Request carries the internal HTTP method, path, body, and query selected by a
// domain router. This package does not own route membership, HTTP dispatch,
// authorization, transport behavior, or query execution.
package routecontract
