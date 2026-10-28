// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// CoreReadContractsVersion versions the core-owned read-contract set the
// eshu-api extraction consumes (Refs eshu-hq/eshu#8872): the GraphQuery and
// ContentStore ports declared in ports.go (plus the related content models),
// and the Postgres ReadStore reader surface. Read runtimes display core
// truth; they never take direct table or queue handles. TestCoreReadContractsVersioned
// in api_boundary_test.go enforces the bump: it pins the frozen method set of
// each port, so a breaking change fails there until this version moves in the
// same diff.
const CoreReadContractsVersion = "v0"
