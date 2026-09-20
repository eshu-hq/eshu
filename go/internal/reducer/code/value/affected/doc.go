// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package affected answers the value-flow refresh emit gate (issue #6785):
// which repos own at least one Function calling a cloud action, constrained
// by producer-written keys. The producers change only graph edges, so a
// producer run whose rows touch no cloud-calling repo can never grow a cloud
// sink; the ACK emits a completion event only when ShouldEmitRefresh passes
// (positive CanonicalWrites plus an absent or positive signal).
//
// The three statements (one shared by the principal and resource entry points)
// read the same INVOKES_CLOUD_ACTION + RUNS_IN shape the
// fixpoint's CloudSinkWorkloadRowsCypher reads, as raw rows deduped in Go —
// no aggregates, DISTINCT, WITH, or subscripts, per the NornicDB shapes #6690
// misanswered. Gate read errors fail OPEN at the call site (a spurious
// refresh is a bounded extra solve; a missed one is silent accuracy loss).
package affected
