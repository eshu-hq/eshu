// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newBootstrapCanonicalGate builds the single in-flight permit pool that
// bounds bootstrap-index's concurrent canonical NornicDB writes (issue #4515,
// Lane B). It reads ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT first and falls
// back to the shared ESHU_GRAPH_WRITE_MAX_IN_FLIGHT knob (via
// graphbackpressure.ClassMaxInFlight), matching the reducer's canonical-class
// knob precedence so an operator who already tuned the reducer's canonical
// ceiling gets identical behavior for bootstrap-index. A non-positive result
// (both env vars unset or non-positive) yields a nil gate, and
// graphbackpressure.WrapExecutorWithGate treats a nil gate as a passthrough.
//
// This gate MUST be constructed exactly once per bootstrap-index run and
// threaded into bootstrapCanonicalExecutorForGraphBackend as the single
// shared instance every canonical write draws a permit from. bootstrap-index
// builds its canonical writer once in openBootstrapCanonicalWriter (called
// once from openBootstrapGraph), and that one writer/executor is shared by
// every projector.Service worker goroutine and by the per-phase-group
// concurrent chunk fan-out in executeEntityPhaseGroupConcurrently, so a
// single gate here already bounds the whole run's in-flight canonical writes
// — no per-worker or per-call gate construction is needed or correct.
func newBootstrapCanonicalGate(getenv func(string) string, instruments *telemetry.Instruments) *sourcecypher.BackpressureGate {
	return graphbackpressure.NewGate(
		graphbackpressure.ClassMaxInFlight(getenv, graphbackpressure.CanonicalMaxInFlightEnv),
		instruments,
		graphbackpressure.CanonicalGateName,
	)
}
