// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package goldengate is the pure, importable assertion core of Eshu's B-7
// golden end-to-end corpus gate (issue #3800). It holds the typed view of the
// B-12 golden snapshot contract (Snapshot and its nested tolerance/correlation
// types), the Finding/Report accumulator, and every Evaluate* function that
// turns an observed value (a node count, an edge count, a drain reading, a query
// response body, a wall-clock elapsed) plus the snapshot contract into a
// Finding — with no I/O.
//
// Keeping the assertions here, free of Postgres, the graph backend, and the
// HTTP API, serves two consumers from one source of truth:
//
//   - cmd/golden-corpus-gate wires the live pipeline (real Postgres, real
//     NornicDB over Bolt, a running eshu-api/eshu-mcp-server), reads observed
//     values from those systems, and feeds them to these functions.
//   - the out-of-tree contributor conformance suite (go/conformance, issue
//     #4112 / R-10) replays a committed cassette through the offline
//     materialization seam with zero provider credentials and zero Docker,
//     derives the same observed values in memory, and feeds them to these same
//     functions.
//
// Because both call the identical Evaluate* logic, the contributor's
// credential-free proof and the in-repo gate cannot drift: there is no forked
// assertion copy to keep in sync. The required/advisory split, the
// presence-positive node-property semantics, and the absence-zero edge-property
// semantics are documented on the individual types and functions.
package goldengate
