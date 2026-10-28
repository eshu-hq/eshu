// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// CoreReadContractsVersion versions the core-owned read-contract set the
// eshu-api extraction consumes (Refs eshu-hq/eshu#8872): the GraphQuery and
// ContentStore ports declared in ports.go (plus the related content models),
// and the Postgres ReadStore reader surface. Read runtimes display core
// truth; they never take direct table or queue handles. The bump is enforced
// by two frozen-set tests: TestCoreReadContractsVersioned here pins
// GraphQuery and ContentStore, while TestReadStoreContractIsVersioned in
// storage/postgres/db pins ReadStore. A breaking change to any named port
// fails its test until this version moves in the same diff, keeping the
// eshu-api boundary explicit.
const CoreReadContractsVersion = "v0"
