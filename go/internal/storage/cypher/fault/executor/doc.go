// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package executor owns the test-only fault-injection decorator for the
// Cypher Executor seam: FaultingExecutor, which applies scripted
// fail-graph-write-once-then-succeed and restart-backend-between-phase-groups
// faults from the Layer 4 fault-script vocabulary
// (go/internal/replay/faultreplay) under the ifafaultinjection build tag,
// and a no-op NewFaultingExecutor stub under every other tag so normal
// builds link no fault machinery at all.
//
// The decorator implements the Executor, GroupExecutor, PhaseGroupExecutor,
// and ProbeExecutor surfaces of the parent cypher package by delegating to
// a wrapped inner executor; every capability it does not script fails
// closed. Wiring lives in go/cmd/reducer's ifa_fault_wiring.go (same build
// tag); the deferred Docker gate scripts/verify-ifa-fault-injection.sh is
// the live proof that scripted faults drive real recovery.
package executor
