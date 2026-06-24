// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// CodeDataflowScannedFactKind marks one scope generation in which the value-flow
// gate (ESHU_EMIT_DATAFLOW) ran, emitted once per generation regardless of
// whether any taint or interproc findings were produced. It carries no findings;
// it is a reconciliation signal so the reducer projects the value-flow evidence
// domains — and therefore retracts stale evidence — even when the current
// generation's finding set is empty. Absent when the gate is off, so the snapshot
// is byte-identical when value-flow emission is disabled.
const CodeDataflowScannedFactKind = "code_dataflow_scanned"
