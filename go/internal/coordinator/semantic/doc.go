// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semantic implements the egress-gated semantic-provider execution
// worker.
//
// ProviderWorker claims semantic extraction jobs, re-checks egress fail-closed
// before any provider dispatch, and emits redacted governance audit events and
// a claim counter for every decision. The worker is disabled by default and
// performs no network I/O: it dispatches only when both the worker's
// ExecutionEnabled flag and the caller-supplied ProviderClient's Enabled
// method report true. The default DisabledProviderClient never permits
// dispatch. The parent coordinator package owns service wiring, scheduling
// position, and the durable claim store the worker drains.
package semantic
