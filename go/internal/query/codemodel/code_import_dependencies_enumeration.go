// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

// Cycle enumeration stop reasons reported in
// coverage.cycle_enumeration_stop_reason (#7346).
const (
	// CycleStopNone means the walk visited every reachable hop within its bounds.
	CycleStopNone = "none"
	// CycleStopCycleCap means the walk stopped at importCycleEnumerationCap
	// closed cycles.
	CycleStopCycleCap = "cycle_cap"
	// CycleStopStepBudget means the walk stopped after examining
	// importCycleEnumerationStepBudget hops.
	CycleStopStepBudget = "step_budget"
)

// importCycleEnumerationStepBudget bounds the hops one cycle enumeration may
// examine (#7346). importCycleEnumerationCap counts closed cycles only, so on
// its own it does not bound the walk: a dense graph that closes few cycles
// still costs time exponential in path length. The budget counts work done, is
// deterministic (a time budget would make the same request answer differently
// on a slower host), and runs in-process, so it serializes nothing.
//
// The value is set by the wall-time ceiling, not by a measured coverage margin.
// Wall time: a dense component costs about 530 ns per examined hop on the
// development laptop (BenchmarkEnumerateImportCyclesDenseComponent), which puts
// 250,000 steps near 130 ms against a 250 ms ceiling. That per-step figure is a
// local smoke number from a contended host, not accepted timing; confirm it on
// the remote before relying on the ceiling.
//
// Coverage is modelled, not measured. The real edge list of the largest Python
// corpus (626 files, 172 imports resolved in-repo) was never walked. Uniform
// random placements of that many edges need at most 9 steps, but a uniform graph
// has almost no strongly connected component and says little about real import
// graphs, which cluster. Package-clustered models (see the walk tests) need about
// 8,000 steps to complete, and denser ones reach the 1,000-cycle cap near 17,000
// to 38,000 steps, before this budget. So the budget is comfortably above every
// modelled shape, but no margin over a real corpus is established. A graph dense
// enough to exhaust it stops with stop reason step_budget: a partial list that
// says why.
const importCycleEnumerationStepBudget = 250_000

// CycleEnumeration reports how one file_import_cycles enumeration ended. The
// handler carries it into the response so a partial cycle list always says why
// it is partial.
type CycleEnumeration struct {
	// Truncated is true when the walk stopped before exhausting its bounds, for
	// any StopReason other than CycleStopNone.
	Truncated bool
	// StopReason is one of the CycleStop constants.
	StopReason string
	// StepBudget is the step budget the walk ran under.
	StepBudget int
	// StepsExamined is the number of hops the walk examined, for the operator
	// span and for tests that pin the walk's cost.
	StepsExamined int
}
