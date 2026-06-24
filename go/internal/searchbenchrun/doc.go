// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchbenchrun is the live execution layer for the design-430 search
// benchmark gate.
//
// The searchbench package owns the pure evidence, suite, and scoring contracts
// and performs no I/O. This package drives a bounded searchretrieval.Backend
// across a searchbench.QuerySuite, measures query latency from runner
// observations, scores the results with searchbench.ScoreQuerySuite, and merges
// operator-measured backend metadata into a searchbench.BackendRun. It then
// assembles validated searchbench.Evidence so the Postgres-vs-NornicDB decision
// can be recorded before any runtime search change.
//
// Nothing here enables NornicDB search, adds an API or MCP route, or writes the
// canonical graph. It is benchmark plumbing: search rank and score stay derived
// retrieval evidence, never canonical truth.
package searchbenchrun
