// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// CoreReadContractsVersion versions the core-owned read-contract set the
// eshu-api extraction consumes (Refs eshu-hq/eshu#8872): the GraphQuery and
// ContentStore ports below plus the Postgres ReadStore reader surface. Read
// runtimes display core truth; they never take direct table or queue handles,
// so a breaking change to any of these ports must bump this version in the
// same diff and keep the eshu-api boundary explicit.
const CoreReadContractsVersion = "v0"
