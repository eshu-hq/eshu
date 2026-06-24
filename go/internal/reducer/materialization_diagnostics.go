// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// materialization_diagnostics.go centralizes the per-domain diagnostic signals
// emitted by the long-pole materialization handlers (issue #3624). The signals
// let an operator distinguish three states from the reducer result log line
// without a pprof profile:
//
//   - upstream ordering stall: input_ready=0, written_rows=0
//     (the handler ran before its inputs existed, so it had nothing to project)
//   - genuine empty work: input_ready=1, written_rows=0
//     (inputs were present but produced no canonical rows after filtering)
//   - normal work: input_ready=1, written_rows>0
//
// These two values are NOT durations. They are routed through Result.SubSignals
// (emitted by the service layer as sub_signal_<key>, no _seconds suffix) so an
// operator never misreads written_rows=42 as 42 seconds. Phase wall-times stay
// in Result.SubDurations (emitted as sub_duration_<key>_seconds).

const (
	// diagnosticSignalInputReady is the SubSignals key encoding whether the
	// handler's upstream input was present (1.0) or absent (0.0). Absent input
	// means an ordering stall: the handler was scheduled before the data it
	// consumes existed. Present input with zero written rows is genuine empty
	// work, not a stall.
	diagnosticSignalInputReady = "input_ready"

	// diagnosticSignalWrittenRows is the SubSignals key carrying the count of
	// canonical rows (or durable intent rows) the handler produced this run.
	// It is a count, not a duration.
	diagnosticSignalWrittenRows = "written_rows"
)

// materializationDiagnosticSignals builds the uniform diagnostic SubSignals map
// shared by every long-pole materialization domain (issue #3624). Defining it
// once guarantees both signals are always set with identical keys and encoding,
// so the input_ready / written_rows contract cannot drift between domains.
//
// inputReady is true when the handler's upstream input was present (request
// entity keys for writer-based domains, or a non-empty projection context for
// fact-loading domains) and false on an ordering stall. writtenRows is the
// count of canonical or durable intent rows produced this run.
func materializationDiagnosticSignals(inputReady bool, writtenRows int) map[string]float64 {
	ready := 0.0
	if inputReady {
		ready = 1.0
	}
	return map[string]float64{
		diagnosticSignalInputReady:  ready,
		diagnosticSignalWrittenRows: float64(writtenRows),
	}
}
