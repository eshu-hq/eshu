// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package askwiring builds the Ask Eshu engine and returns a wired
// [query.AskHandler] for use by both the API server and the MCP server.
//
// It is the single source of truth for engine construction, narration-posture
// derivation, and default-off semantics. Both cmd/api and cmd/mcp-server
// import it so the wiring is kept DRY while keeping each binary in its own
// package main.
package askwiring
