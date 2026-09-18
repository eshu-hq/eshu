// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tempo collects bounded live Tempo trace-signal metadata.
//
// The package owns source observation for configured Tempo query-frontends and
// emits observability source facts for tag names, bounded tag-value metadata,
// source instances, and source-local warnings. It deliberately avoids trace
// retrieval, trace search payloads, raw trace IDs, spans, request attributes,
// TraceQL bodies, tenant IDs, and tag values. Reducers and query surfaces own
// correlation against declared/applied observability evidence. The package
// records bounded Tempo observe/fetch spans and low-cardinality counters for
// provider requests, emitted facts, retries, rate limits, redactions,
// high-cardinality rejection, stale evidence, and fetch duration.
package tempo
