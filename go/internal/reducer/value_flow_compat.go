// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/reducer/valueflow"
)

// This file is the transitional compatibility surface for the value-flow
// fixpoint family that moved to [valueflow] (issue #6061). Reducer-root call
// sites keep their current spelling; each entry is deleted once its last
// caller has moved into a family subpackage.
//
// code_value_flow_stale_cleanup_runner.go and
// code_value_flow_backfill_state_marker.go stay in root: the first has no
// dependency on the moved family (it only reaches codetaint's writer/ledger
// surface), and the second's only real caller is the still-in-root
// projected_source_edge_backfill family, so neither belongs in valueflow.

// GraphValueFlowCloudSinkTargetLoader loads graph-backed cloud sink edges for
// the value-flow fixpoint. See [valueflow.GraphValueFlowCloudSinkTargetLoader].
type GraphValueFlowCloudSinkTargetLoader = valueflow.GraphValueFlowCloudSinkTargetLoader

// ValueFlowCloudSinkTargetsCypher is the bounded Cypher query cloud sink
// target loading runs. See [valueflow.ValueFlowCloudSinkTargetsCypher].
const ValueFlowCloudSinkTargetsCypher = valueflow.ValueFlowCloudSinkTargetsCypher

// ValueFlowFixpointComponentStore is the durable weak-component cache store
// port. See [valueflow.ValueFlowFixpointComponentStore].
type ValueFlowFixpointComponentStore = valueflow.ValueFlowFixpointComponentStore

// NewValueFlowFixpointCache forwards to [valueflow.NewValueFlowFixpointCache].
func NewValueFlowFixpointCache() *valueflow.ValueFlowFixpointCache {
	return valueflow.NewValueFlowFixpointCache()
}

// ValueFlowProgramInput is the bounded in-memory snapshot used to assemble a
// value-flow Program. See [valueflow.ValueFlowProgramInput].
type ValueFlowProgramInput = valueflow.ValueFlowProgramInput

// ValueFlowCallEdge is one active code-call edge used by Program assembly.
// See [valueflow.ValueFlowCallEdge].
type ValueFlowCallEdge = valueflow.ValueFlowCallEdge

// ValueFlowProgramAssemblyStats summarizes one Program assembly cycle. See
// [valueflow.ValueFlowProgramAssemblyStats].
type ValueFlowProgramAssemblyStats = valueflow.ValueFlowProgramAssemblyStats

// BuildValueFlowProgram forwards to [valueflow.BuildValueFlowProgram].
func BuildValueFlowProgram(input ValueFlowProgramInput) (interproc.Program, ValueFlowProgramAssemblyStats) {
	return valueflow.BuildValueFlowProgram(input)
}

// FunctionSummarySnapshotLoader reloads durable value-flow summaries for the
// cross-repo fixpoint. See [valueflow.FunctionSummarySnapshotLoader].
type FunctionSummarySnapshotLoader = valueflow.FunctionSummarySnapshotLoader

// FunctionSourceSnapshotLoader reloads durable value-flow source ports for
// the cross-repo fixpoint. See [valueflow.FunctionSourceSnapshotLoader].
type FunctionSourceSnapshotLoader = valueflow.FunctionSourceSnapshotLoader

// FunctionGraphIDSnapshotLoader reloads durable FunctionID->Function.uid
// mappings. See [valueflow.FunctionGraphIDSnapshotLoader].
type FunctionGraphIDSnapshotLoader = valueflow.FunctionGraphIDSnapshotLoader

// ValueFlowFixpointEvidenceLoader composes durable function summaries,
// source ports, graph ids, and graph-backed cloud sink targets into the
// existing code_interproc_evidence reducer input. See
// [valueflow.ValueFlowFixpointEvidenceLoader].
type ValueFlowFixpointEvidenceLoader = valueflow.ValueFlowFixpointEvidenceLoader

// ValueFlowFixpointEvidenceProjector writes summary-fixpoint findings as
// TAINT_FLOWS_TO evidence. See [valueflow.ValueFlowFixpointEvidenceProjector].
type ValueFlowFixpointEvidenceProjector = valueflow.ValueFlowFixpointEvidenceProjector

// ValueFlowFixpointProjectionResult records the visible outcome of a
// post-summary fixpoint projection. See
// [valueflow.ValueFlowFixpointProjectionResult].
type ValueFlowFixpointProjectionResult = valueflow.ValueFlowFixpointProjectionResult
