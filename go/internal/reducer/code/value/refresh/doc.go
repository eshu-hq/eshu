// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package refresh re-runs the global value-flow fixpoint after late producers
// land (issue #6785). [Handler] executes one code_value_flow_refresh intent by
// calling only the fixpoint projector: summaries, sources, and graph ids are
// unchanged by the producers whose completion enqueues the refresh, and the
// fixpoint reloads them globally before solving.
//
// The refresh item is a singleton anchored to the migration-seeded eshu:global
// scope, so producer completions in any later generation reopen the same row
// through the existing durable completion fanout — including, since issue
// #6923, code_function_summary's own completion, which stopped solving the
// fixpoint inline and became the fifth producer instead.
//
// Before that solve runs, [Handler.checkInputsLiveness] fences it (#6923):
// while [InputsLiveness] reports a pending writer of the cloud-sink chain on
// an active generation, Handle refuses (Retryable, non-counting
// value_flow_inputs_not_ready) instead of reading partial graph state, until
// either the fence clears or elapsed time since the singleton's own cycle
// anchor reaches crossscope.ProducerReadinessMaxWait, at which point it
// solves anyway. This is what collapsed the concurrent-writer race the
// #6785 design note used to file separately (issue #6880): every trigger of
// the global solve now coalesces onto this one fenced singleton, so there is
// one writer, not several racing ones.
package refresh
