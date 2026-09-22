// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// DataflowScannedFactKind marks one scope generation in which the value-flow
// gate (ESHU_EMIT_DATAFLOW) ran, emitted once per generation regardless of
// whether any taint or interproc findings were produced. It carries no findings;
// it is a reconciliation signal so the reducer projects the value-flow evidence
// domains — and therefore retracts stale evidence — even when the current
// generation's finding set is empty. Absent when the gate is off, so the snapshot
// is byte-identical when value-flow emission is disabled.
const DataflowScannedFactKind = "code_dataflow_scanned"

// DataflowFunctionFactKind identifies one parser-emitted function-level
// dataflow record carrying bounded CFG, reaching-definition, and
// control-dependence facts. It is exact parser evidence for API/MCP code-flow
// readbacks, not canonical graph truth. Absent when the value-flow gate is off.
const DataflowFunctionFactKind = "code_dataflow_function"
