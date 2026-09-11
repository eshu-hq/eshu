// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package value solves the cross-repo value-flow fixpoint that produces
// the reducer/code-interproc-fixpoint TAINT_FLOWS_TO evidence source: a
// distinct, generation-independent evidence stream kept separate from the
// direct code_interproc_evidence rows taint materializes per generation
// (issue #6061).
//
// [FixpointEvidenceLoader] composes durable function summaries
// (FunctionSummarySnapshotLoader), param sources (FunctionSourceSnapshotLoader),
// the FunctionID->graph-uid map (FunctionGraphIDSnapshotLoader), and
// graph-backed cloud sink targets ([GraphCloudSinkTargetLoader],
// FunctionCloudSinkTargetLoader) into a [interproc.Program], solves it
// (optionally through a durable [FixpointComponentStore] so a
// reducer restart or second replica reuses unchanged weak components instead
// of resolving the whole corpus), and resolves finding endpoints through the
// graph-uid map. [FixpointEvidenceProjector] then retracts and
// rewrites the full fixpoint-owned evidence source through
// taint.CodeInterprocEvidenceWriter/CodeInterprocProjectedEdgeLedger,
// using taint.ExtractCodeInterprocFixpointEvidenceRows' separate uid
// namespace so a fixpoint-solved edge can never collide with a direct-fact
// edge in the graph writer's MERGE.
//
// [FixpointCache] caches solved weakly-connected components inside
// the reducer process, keyed by component membership, durable summary
// content versions, and external source/sink inputs; [NewFixpointCache]
// constructs an empty one. When one function summary version changes, only
// the component that can carry taint from that function is recomputed —
// unrelated components reuse their cached findings before the final global
// sort/cap and evidence rewrite. [SolveSnapshotIncrementalDurable]
// partitions durable summary/source/sink state before Program assembly and
// persists solved components behind the same component-content cache key, so
// a cold cache (restart, second replica) still avoids reassembling or
// resolving unchanged components.
//
// [BuildProgram] assembles a bounded [interproc.Program] from active
// CALLS evidence, persisted function summaries, and durable param-source
// rows without solving or writing graph evidence; [ProgramAssemblyRunner]
// can drive it over a bounded batch of [ProgramInputLoader] inputs
// for diagnostics, but is not yet wired into cmd/reducer's production path —
// production assembly happens inline inside
// [FixpointEvidenceLoader.LoadCodeInterprocEvidence].
//
// [GraphCloudSinkTargetLoader] loads graph-backed cloud sink edges
// (Function -[:INVOKES_CLOUD_ACTION]-> CloudAction, joined through an exact
// single RUNS_IN workload fan-out and WorkloadInstance USES CloudResource
// principal to a matching CAN_PERFORM action; ambiguous runtime identity
// stays empty) for functions already known to the fixpoint's Function.uid
// snapshot, via [CloudSinkTargetsCypher]. A cloud sink bridge is
// attached only to observed parameter ports for that FunctionID — a graph
// edge without parameter evidence stays visible as no value-flow finding
// rather than fabricating precision.
//
// BackfillStateMarker (backfill_state_marker.go) moved here from the reducer
// root with the code/ tree move (#6609). Nothing in this package calls it; its
// only caller is the root's projected_source_edge_backfill family, which names
// it through the CodeValueFlowBackfillStateMarker alias. This package does not
// own code_value_flow_stale_cleanup_runner.go, which stays in the reducer
// root: it is a side runner that needs the root PartitionLeaseManager and only
// reaches taint's writer/ledger surface.
package value
