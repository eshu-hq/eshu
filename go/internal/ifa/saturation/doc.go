// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package saturation is Ifá's Layer 3 saturation Odù (issue #4579, parent epic
// #4389, ADR docs/internal/design/4389-ifa-conformance-platform.md "Layer 3").
//
// It drives more recoverable graph writes than a permit pool admits and asserts
// the #3560 failure SHAPE rather than mere survival: the real
// cypher.BackpressureGate engages (its wait observer fires), over-pool work
// waits instead of executing, nothing dead-letters spuriously, and the queue
// drains to the B-12 residual (zero non-terminal work) after pressure releases.
// It is the permanent regression proof for the #3560 failure class — a slow
// graph backend dead-lettering recoverable work — that
// go/internal/storage/cypher/backpressure_executor.go's gate exists to prevent.
//
// This package deliberately lives beside the pure, deterministic contract-layer
// go/internal/ifa (which forbids wall-clock, concurrency, and storage seams):
// the saturation Odù is a runtime scenario over the real permit gate, so it
// belongs in a sibling subpackage exactly as go/internal/ifa/graphdump isolates
// its graph concern. It reuses go/internal/ifa's DeadLetterRecord /
// DeadLetterSetsEqual comparator for the "no spurious dead letters" assertion so
// there is one dead-letter vocabulary across Ifá, and it exercises the real
// cypher.GraphWriteTimeoutError + reducer.IsRetryable classification so the
// regression tracks the production retry-vs-dead-letter decision, not a copy.
//
// It is hermetic and credential-free: no Postgres, no graph backend, no
// network. The backend is a capacity-bounded model whose over-subscription
// timeout is the real cypher.GraphWriteTimeoutError, so the gate's bound
// (in-flight <= permit pool) is what keeps a capacity-C backend from ever
// emitting a timeout — the exact mechanism the #3560 gate provides.
package saturation
