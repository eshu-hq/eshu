// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workload finds the name-only Workload nodes the repository
// scoped identity change (#5385) should have removed, and says whether each
// one is safe to retire (#8795).
//
// A leftover has the exact legacy handle id (workload:<name>) and no
// generation_id. It is safe to retire only when no WorkloadInstance
// references it (by INSTANCE_OF edge or by workload_id property), a
// generation-stamped repository-scoped twin with the same name and repo_id
// exists and the same repository defines it, and every relationship on the
// leftover is a DEFINES from its own repository or a SAME_NAME to another
// Workload.
//
// [Detect] reads the candidates and the facts about them with single-clause,
// identity-anchored statements and [Evaluate] decides in Go, so the readiness
// check, the admin dry run, and the pre-delete verification share one code
// path and one answer. [DeleteNeo4jCypher] is the Neo4j delete statement; it
// takes each node's write lock and then re-evaluates every clause itself. Its
// comment states exactly which concurrent writers that does and does not
// cover.
//
// The package depends only on the standard library and workloadid. It runs no
// writes and emits no telemetry; the reducer's workload/legacy/retire package
// owns the execution, counters, and logs.
package workload
