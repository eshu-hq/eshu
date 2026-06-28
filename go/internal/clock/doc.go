// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package clock provides the injectable time source for the reducer queue,
// lease, and reap path.
//
// Clock is a one-method interface (Now). Production wiring injects System(),
// whose Now() is exactly time.Now(), so replacing a nil-fallback seam with
// System() changes no behavior. Deterministic replay (epic #4102, Layer 3) and
// tests inject a Simulated clock whose Advance and Set move time forward
// without sleeping, making lease expiry, claim visibility, and retry-backoff
// timing deterministic.
//
// The storage layer keeps its established `Now func() time.Time` struct seams;
// NowFunc adapts a Clock to that field (assign clk.Now), and a single Simulated
// shared across components advances them together. Simulated is safe for
// concurrent use and rejects backward time to preserve the monotonic-deadline
// assumptions the lease path depends on.
package clock
