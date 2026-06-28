// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package mcpreplay records canonicalized golden MCP tool-call responses and
// replay-asserts them offline (epic #4102, R-9). It is the MCP read-surface
// half of the deterministic replay integration gate: it proves a tool handler
// or response shape change is caught before it reaches MCP callers, without a
// live graph backend, Postgres, or LLM.
//
// A recording is a set of [apirecording.Exchange] values, each pairing an
// [apirecording.RequestDescriptor] (Transport=TransportMCP, tool name in the
// Body as a JSON-RPC tools/call payload) with the canonical response captured
// by driving that call through an in-process [mcp.Server] backed by the query
// handler mux. The recorded body wraps the caller-visible MCP result fields:
// the canonicalized structuredContent under "structured_content" and the
// result.isError classification flag under "is_error", so the gate catches
// both payload-shape drift and refusal/error-classification drift. The
// [apirecording] package owns the recording format; mcpreplay is the MCP-flavor
// driver that produces and asserts recordings at the MCP transport seam.
//
// Answer-parity: when an HTTP API endpoint and an MCP tool answer the same
// query, the goldens are asserted to carry consistent substantive truth. The
// parity check operates on the data field of the canonical envelope, ignoring
// legitimate envelope-level differences (transport metadata, summary text).
//
// Offline means: no Docker, no live graph, no network. Record and Assert drive
// the real MCP dispatch path through the in-process handler supplied by the
// caller. The caller must supply a handler backed by deterministic, in-process
// query logic (such as the query-playbook handler), so no live backend
// dependency is introduced.
package mcpreplay
