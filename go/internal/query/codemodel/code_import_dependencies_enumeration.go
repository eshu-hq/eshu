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
// The value is derived from two measurements. Coverage: the largest measured
// Python corpus (626 files, 172 imports resolved to an in-repo file) walks in at
// most 9 steps at the maximum cycle length across 200 random placements of those
// edges, so the budget sits four orders of magnitude above real use. Wall time:
// a dense component costs about 530 ns per examined hop on the development
// laptop (BenchmarkEnumerateImportCyclesDenseComponent), which puts 250,000
// steps near 130 ms against the 250 ms ceiling. The per-step figure is a local
// smoke number, not accepted timing; confirm it on the remote before relying on
// the ceiling. A corpus in which every import resolves needs about 370,000 steps
// and stops at this budget with stop reason step_budget, which is the intended
// answer for a graph that dense: a partial list that says why.
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
