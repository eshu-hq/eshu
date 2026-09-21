// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package backendconformance defines the graph-backend conformance matrix and
// reusable read/write corpora for Chunk 5 of the embedded local backends ADR.
//
// The package deliberately keeps the default test path free of live database
// requirements. Adapter-specific integration tests can import the same read and
// write corpora, run them against Neo4j, NornicDB, Compose, or remote proof
// environments, and report case results without changing the matrix contract.
//
// Read cases assert either a minimum row count or, through
// [ReadCase.WantRows], the exact rows. The exact-row cases cover the value-flow
// cloud sink statements the reducer runs and the aggregation and
// optional-match shapes that older NornicDB builds answered wrongly with no
// error, so a backend regression on them fails the live run.
//
// Differential recording ([DifferentialRecorder] with the WrapGraphQuery and
// WrapExecutor decorators) captures statement fingerprints and result digests
// per execution for the NornicDB-vs-Neo4j comparison; it stays out of the hot
// path unless ESHU_DIFFERENTIAL_CAPTURE=1.
package backendconformance
