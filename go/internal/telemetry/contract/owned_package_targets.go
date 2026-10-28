// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Owned-package target window read (#9422). The reader that bounds derived
// package-registry and vulnerability-intelligence planning selects one hash
// slot per reconcile window; these names let an operator see T, P, the slot
// and its size on the trace beside the postgres.query child span.
const (
	// SpanOwnedPackageTargetsRead wraps one owned-package hash-slot read,
	// from the statement through row scanning. Its postgres.query child span
	// carries the statement latency.
	SpanOwnedPackageTargetsRead = "owned_package_targets.read"
	// EventOwnedPackageTargetsWindow is the span event one read adds with the
	// window stats (total_targets, slot_count, slot, slot_size, rows_read,
	// selected) and the per-ecosystem selected counts.
	EventOwnedPackageTargetsWindow = "owned_package_targets.window"
)
