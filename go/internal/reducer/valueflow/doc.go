// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package valueflow solves the cross-repo value-flow fixpoint that produces
// the reducer/code-interproc-fixpoint TAINT_FLOWS_TO evidence source: a
// distinct, generation-independent evidence stream kept separate from the
// direct code_interproc_evidence rows codetaint materializes per generation
// (issue #6061).
//
// [ValueFlowFixpointEvidenceLoader] composes durable function summaries
// (FunctionSummarySnapshotLoader), param sources (FunctionSourceSnapshotLoader),
// the FunctionID->graph-uid map (FunctionGraphIDSnapshotLoader), and
// graph-backed cloud sink targets ([GraphValueFlowCloudSinkTargetLoader],
// FunctionCloudSinkTargetLoader) into a [interproc.Program], solves it
// (optionally through a durable [ValueFlowFixpointComponentStore] so a
// reducer restart or second replica reuses unchanged weak components instead
// of resolving the whole corpus), and resolves finding endpoints through the
// graph-uid map. [ValueFlowFixpointEvidenceProjector] then retracts and
// rewrites the full fixpoint-owned evidence source through
// codetaint.CodeInterprocEvidenceWriter/CodeInterprocProjectedEdgeLedger,
// using codetaint.ExtractCodeInterprocFixpointEvidenceRows' separate uid
// namespace so a fixpoint-solved edge can never collide with a direct-fact
// edge in the graph writer's MERGE.
//
// [ValueFlowFixpointCache] caches solved weakly-connected components inside
// the reducer process, keyed by component membership, durable summary
// content versions, and external source/sink inputs; [NewValueFlowFixpointCache]
// constructs an empty one. When one function summary version changes, only
// the component that can carry taint from that function is recomputed —
// unrelated components reuse their cached findings before the final global
// sort/cap and evidence rewrite. [SolveValueFlowSnapshotIncrementalDurable]
// partitions durable summary/source/sink state before Program assembly and
// persists solved components behind the same component-content cache key, so
// a cold cache (restart, second replica) still avoids reassembling or
// resolving unchanged components.
//
// [BuildValueFlowProgram] assembles a bounded [interproc.Program] from active
// CALLS evidence, persisted function summaries, and durable param-source
// rows without solving or writing graph evidence; [ValueFlowProgramAssemblyRunner]
// can drive it over a bounded batch of [ValueFlowProgramInputLoader] inputs
// for diagnostics, but is not yet wired into cmd/reducer's production path —
// production assembly happens inline inside
// [ValueFlowFixpointEvidenceLoader.LoadCodeInterprocEvidence].
//
// [GraphValueFlowCloudSinkTargetLoader] loads graph-backed cloud sink edges
// (Function -[:INVOKES_CLOUD_ACTION]-> CloudAction, joined through an exact
// single RUNS_IN workload fan-out and WorkloadInstance USES CloudResource
// principal to a matching CAN_PERFORM action; ambiguous runtime identity
// stays empty) for functions already known to the fixpoint's Function.uid
// snapshot, via [ValueFlowCloudSinkTargetsCypher]. A cloud sink bridge is
// attached only to observed parameter ports for that FunctionID — a graph
// edge without parameter evidence stays visible as no value-flow finding
// rather than fabricating precision.
//
// This package does not own code_value_flow_stale_cleanup_runner.go or
// code_value_flow_backfill_state_marker.go, which stay in the reducer root:
// the stale-cleanup runner only reaches codetaint's writer/ledger surface
// (it has no dependency on anything in this package), and the backfill state
// marker's only real caller is the still-in-root
// projected_source_edge_backfill family.
package valueflow
