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
// One pair of cases is opt-in. The value-flow cloud sink read and seed
// reproduce defects that are open upstream, so they are included only when
// ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is set to 1, true, or yes. They are
// absent from the corpora by default rather than present-and-skipped, which
// means [DefaultReadCorpus] and [DefaultWriteCorpus] vary in length with the
// process environment. A live run that omits them says so in its output.
package backendconformance
