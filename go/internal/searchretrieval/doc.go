// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchretrieval defines the bounded internal retrieval contract for
// semantic evaluation.
//
// The package validates query scope, limit, timeout, and search mode before any
// backend adapter can run. Runner executes that request through a narrow backend
// port with request timeout enforcement and emits one Observation summary for
// the attempt. The package normalizes ranked searchdocs.Document candidates
// into deterministic top-K responses that preserve derived truth labels,
// freshness, graph handles, truncation, and false-canonical-claim counts. It
// performs no Postgres, graph, NornicDB, HTTP, MCP, or OTEL I/O.
package searchretrieval
