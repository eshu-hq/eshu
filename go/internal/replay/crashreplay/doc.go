// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package crashreplay is the Layer 3 (recovery) crash-point replay for the
// deterministic replay framework (design doc 4102, R-14 / #4123).
//
// It drives recorded projection work through the real reducer service loop,
// injects a crash at a controlled checkpoint, drops the in-memory worker while
// keeping the durable store, replays the remainder from that durable state, and
// asserts the recovered canonical graph equals the no-crash graph with no work
// item completed twice. This turns crash recovery — historically specced but
// never gated (#1289) — into a scripted, deterministic point inside a replay.
//
// The durable state is a durableStore: an in-memory stand-in for the Postgres
// fact_work_items + lease rows the reducer queue claims from. It is the only
// state that survives the crash, mirroring committed rows surviving a process
// death. It hands out pending items and reclaims items whose lease has lapsed on
// an injected clock.Clock (R-12, #4121); each (re)claim bumps a fencing token
// (the attempt count) so a recovery reclaim is observably a new attempt. Work
// items and the canonical graph come from the R-13 schedulereplay package
// (#4122), which loads them from the committed offline-tier cassette through the
// real cassette -> materialization seam.
//
// Two crash checkpoints are scripted (CrashKind):
//
//   - CrashBeforeClaim — a clean boundary after N items are durably completed
//     and before the next is claimed. No lease is held across the crash; it
//     proves recovery never redoes already-completed work.
//   - CrashAfterApply — the dirty post-lease-pre-complete window: the crash item
//     is projected to the graph and then the worker dies before the ack, leaving
//     a held lease. Recovery must wait out the lease on the simulated clock,
//     reclaim under a higher fencing token, and idempotently re-project it.
//
// A crash is simulated by panicking a private sentinel from a claim/execute
// decorator; the run goroutine recovers exactly that sentinel and re-raises any
// other panic, so a real bug is never swallowed. Crash runs are single-worker so
// the panic is deterministic and the snapshot is byte-stable. The real
// concurrent FOR UPDATE SKIP LOCKED claim path under genuine Postgres contention
// is deliberately out of scope here — it is the irreducible remainder covered by
// R-15's real-Postgres contention gate (design doc 4102 §10.1).
//
// The package requires no Postgres, no graph backend, and no Docker, so the
// crash-recovery gate runs in the default `go test` pass on every PR.
package crashreplay
