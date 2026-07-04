// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// bootstrapGraphWriteGate holds the single in-flight permit pool that bounds
// bootstrap-index's canonical graph writes (issue #4515, Lane B). Unlike the
// reducer (which has independent canonical and semantic write classes, see
// cmd/reducer/graph_write_backpressure_wiring.go), bootstrap-index only ever
// drives canonical source-local writes, so it needs exactly one gate and no
// aggregate/per-class split.
//
// canonicalGate is nil-tolerant: a non-positive ceiling (the default —
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT and ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT
// both unset) makes boundExecutor return its inner executor unchanged, so this
// wiring is a safe no-op until an operator opts in. A prior attempt at this
// wiring found a confirmed blocker: bootstrap's canonical NornicDB executor
// (bootstrapNornicDBPhaseGroupExecutor) implements
// sourcecypher.PhaseGroupExecutor (ExecutePhaseGroup), not
// sourcecypher.GroupExecutor, so wrapping it required extending
// graphbackpressure.WrapExecutorWithGate with a phase-group-aware case (see
// go/internal/storage/cypher/backpressure_executor.go and
// go/internal/graphbackpressure/backpressure.go) before this wiring could be
// added without silently degrading every bootstrap canonical write to
// per-statement sequential execution.
type bootstrapGraphWriteGate struct {
	canonicalGate *sourcecypher.BackpressureGate
}

// newBootstrapGraphWriteGate builds the canonical permit pool from the
// environment. It reads ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT first and
// falls back to the shared ESHU_GRAPH_WRITE_MAX_IN_FLIGHT knob (via
// graphbackpressure.ClassMaxInFlight), matching the reducer's canonical-class
// knob precedence so an operator who already tuned the reducer's canonical
// ceiling gets identical behavior for bootstrap-index. A non-positive result
// (both env vars unset or non-positive) yields a nil gate, so boundExecutor is
// a passthrough by default.
func newBootstrapGraphWriteGate(getenv func(string) string, instruments *telemetry.Instruments) bootstrapGraphWriteGate {
	return bootstrapGraphWriteGate{
		canonicalGate: graphbackpressure.NewGate(
			graphbackpressure.ClassMaxInFlight(getenv, graphbackpressure.CanonicalMaxInFlightEnv),
			instruments,
			graphbackpressure.CanonicalGateName,
		),
	}
}

// boundExecutor wraps inner so it draws a canonical permit before delegating.
// It must be applied OUTERMOST — after any timeout/retry adapter — so a permit
// covers the whole write attempt (retries and the ESHU_CANONICAL_WRITE_TIMEOUT
// deadline) and permit-wait never counts against that timeout. A disabled gate
// (the default) returns inner unchanged. WrapExecutorWithGate preserves
// whichever of GroupExecutor or PhaseGroupExecutor inner implements, so
// bootstrap's NornicDB canonical executor (PhaseGroupExecutor) and its Neo4j
// canonical executor (GroupExecutor) both keep their grouped-write path through
// the gate instead of falling back to sequential per-statement execution.
func (g bootstrapGraphWriteGate) boundExecutor(inner sourcecypher.Executor) sourcecypher.Executor {
	return graphbackpressure.WrapExecutorWithGate(inner, g.canonicalGate)
}
