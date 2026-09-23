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
// [WriteCorpusFor] adds the write cases whose Cypher depends on the backend
// dialect. For the semantic :Module write, the statements come from the
// production semantic-entity writer that the reducer wires for that backend.
// The matching reads hold both backends to one correct outcome. A backend
// that does not give it today carries a [BackendOverride]: the rows it does
// return, pinned under a required tracking issue. [RunReadCorpusFor] applies
// the override for the backend it runs as, so both lanes stay deterministic
// and a change on either side fails the live run.
//
// Differential recording ([DifferentialRecorder] with the WrapGraphQuery and
// WrapExecutor decorators) captures statement fingerprints and result digests
// per execution for the NornicDB-vs-Neo4j comparison; it stays out of the hot
// path unless ESHU_DIFFERENTIAL_CAPTURE=1. Comparison groups UNWIND and
// single-use IN-list batches element-wise, names one divergence kind per
// difference (missing, results, executions, failures, rowcount), and
// normalizes run-scoped lineage (generation stamps, derivation-stamped ids)
// plus backend serialization (graph-object identity, list order, wall-clock
// columns) so scheduling and lineage noise never masquerades as a result
// divergence.
package backendconformance
