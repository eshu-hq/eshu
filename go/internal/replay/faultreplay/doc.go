// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package faultreplay is the Layer 4 (deterministic fault injection) fault
// script for the Ifá conformance platform (design doc
// docs/internal/design/4389-ifa-conformance-platform.md, #4580).
//
// This slice (S1) owns the fault-script schema only: versioned, fail-closed
// DATA describing what to break and when to break it. It intentionally has no
// runner and no decorator wiring — those are separate slices (S2/S4) that
// consume a Script this package has already validated.
//
// A Script names one of five fault kinds (kill-worker-after-claim,
// expire-lease-mid-handler, fail-graph-write-once-then-succeed,
// restart-backend-between-phase-groups, fail-terminal) and, for each, a
// Trigger that is always an ordinal over observed events (a claim count, an
// intent's position, a phase-group count) or a stable string ID — never a
// duration, wall-clock timestamp, or random draw. That constraint is what
// keeps a fault run replayable byte-for-byte, like any other Odù run in the
// determinism matrix: a wall-clock trigger would fire at a different point on
// every run, breaking the byte-identical canonical-graph assertion the wider
// Layer 4 gate exists to make.
//
// Parse and Load decode fault-script JSON with DisallowUnknownFields and then
// run Script.Validate, so every Script this package hands back is fail-closed:
// version 1, a known fault kind, a well-formed trigger, and (for
// fail-graph-write-once-then-succeed) a known target lane.
package faultreplay
