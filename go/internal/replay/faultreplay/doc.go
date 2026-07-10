// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package faultreplay is the Layer 4 (deterministic fault injection) fault
// script and hermetic runner for the Ifá conformance platform (design doc
// docs/internal/design/4389-ifa-conformance-platform.md, #4580).
//
// The package has two halves. S1 (script.go) owns the fault-script schema:
// versioned, fail-closed DATA describing what to break and when to break it.
// A Script names one of five fault kinds (kill-worker-after-claim,
// expire-lease-mid-handler, fail-graph-write-once-then-succeed,
// restart-backend-between-phase-groups, fail-terminal) and, for each, a
// Trigger that is always an ordinal over observed events (a claim count, an
// intent's position, a phase-group count) or a stable string ID — never a
// duration, wall-clock timestamp, or random draw. That constraint is what
// keeps a fault run replayable byte-for-byte, like any other Odù run in the
// determinism matrix: a wall-clock trigger would fire at a different point on
// every run, breaking the byte-identical canonical-graph assertion the wider
// Layer 4 gate exists to make. Parse and Load decode fault-script JSON with
// DisallowUnknownFields and then run Script.Validate, so every Script this
// package hands back is fail-closed.
//
// S2 (source.go, executor.go, runner.go) is the hermetic runner: it drives a
// validated Script through the REAL reducer.Service loop with no Docker, no
// Postgres, and no graph backend, decorating only the two seams
// reducer.Service already exposes for this purpose (WorkSource/BatchWorkSource
// and Executor — see go/internal/reducer/service.go:27-56). FaultingWorkSource
// models kill-worker-after-claim and expire-lease-mid-handler as scripted
// redelivery of an already-claimed intent (never a real goroutine kill or a
// real lease timer). FaultingExecutor models fail-graph-write-once-then-
// succeed (both the executor-retry and queue-retry lanes) and fail-terminal.
// RunFault wires both decorators around schedulereplay's in-memory canonical
// graph and asserts, exactly like schedulereplay: the converged snapshot after
// a fault-scripted run must be byte-identical to the fault-free baseline of
// the same work items, and a non-draining or pre-canceled run must return an
// error, never a green partial snapshot. RunFault also verifies, after the
// run drains and before it reports success, that every scripted fault
// actually fired at least once: a trigger that never matches anything real
// (a bad ordinal, a stale intent ID, a non-matching operation_match) is an
// inert script, and RunFault returns an error naming it rather than
// snapshotting the accidentally fault-free graph.
//
// restart-backend-between-phase-groups needs a real graph backend to restart
// and so cannot run hermetically; it is handled by the in-binary cypher
// FaultingExecutor (behind the ifafaultinjection build tag) plus the operator
// harness, not here. This package rejects that fault kind at construction
// rather than silently ignoring it.
package faultreplay
