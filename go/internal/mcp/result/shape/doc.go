// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package resultshape names the two wire layouts of an MCP tools/call result
// and carries a connection's choice through a request context.
//
// Dual is the layout Eshu has always sent: a text summary, the envelope again
// as an embedded resource, and structuredContent. Structured sends one text
// summary and structuredContent, so the payload crosses the wire once. The
// package owns only the value, its parsing, and the context carrier. Rendering
// a result, counting it against the response budget, reading the query
// parameter or environment variable, and recording telemetry stay in
// internal/mcp and cmd/mcp-server.
package resultshape
