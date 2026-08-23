// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "time"

// Result captures the bounded terminal outcome of one reducer execution.
type Result struct {
	IntentID        string
	Domain          Domain
	Status          ResultStatus
	EvidenceSummary string
	CanonicalWrites int
	CompletedAt     time.Time
	// SubDurations holds optional per-phase handler timings (seconds) that the
	// service layer emits alongside handler_duration_seconds. Each key names one
	// handler stage (e.g. "load_inputs", "build_projection", "graph_write",
	// "phase_publish"). Nil or empty means the handler did not populate
	// sub-timings; the service log line omits missing keys rather than
	// emitting zeros. Handlers that adopt sub-timing MUST populate a
	// non-nil map so the log line can distinguish "zero work" from
	// "not instrumented".
	SubDurations map[string]float64
	// SubSignals holds optional non-duration diagnostic signals (counts and
	// flags) that the service layer emits as sub_signal_<key> with NO _seconds
	// suffix. It carries values that are not wall-times — currently
	// "input_ready" (1.0 input present / 0.0 ordering stall) and "written_rows"
	// (canonical or durable intent rows produced). Routing these through a
	// separate field keeps the _seconds suffix honest for SubDurations so an
	// operator never misreads a row count as a duration. Nil or empty means the
	// handler did not populate signals; missing keys are omitted, not zeroed.
	SubSignals map[string]float64
}
