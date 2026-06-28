// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package apirecording records canonicalized golden API responses for Eshu's
// query/API handlers and replay-asserts them offline (epic #4102, R-8). It is
// the read-surface half of the deterministic replay integration gate: it proves
// a query-handler or OpenAPI shape change is caught before it reaches consumers,
// without a live graph backend, Postgres, or LLM.
//
// A recording is a set of Exchanges. Each Exchange pairs a transport-agnostic
// RequestDescriptor (method, path, headers, body) with the canonical response
// captured by driving that request against an http.Handler via httptest. Record
// canonicalizes every response through the shared replay canonical core so a
// re-record is byte-identical when the handler output is shape-equivalent and a
// diff highlights only real shape changes. Assert re-drives the recorded
// requests against a handler and fails with a clear status/body diff when a live
// response diverges from the golden; WriteFile is the -update regeneration path.
//
// The recording format is transport-agnostic on purpose. RequestDescriptor
// carries a Transport, and the MCP dispatch path resolves tool calls through the
// same query handler mux, so R-9 (#4111, MCP tool-call replay) reuses this
// package by recording TransportMCP exchanges without a format change.
//
// Scope is deterministic success and refusal (error-envelope) responses;
// timeout and partial recording are deferred.
package apirecording
