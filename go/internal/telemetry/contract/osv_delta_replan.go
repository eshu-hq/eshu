// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// OSV delta replan (#9777 step 2). The change-driven re-query path admits
// changed (advisory, ecosystem, package) tuples into the owned_target_replan
// queue, drains one slot's extra scopes per pass, and advances the commit
// watermark in the drain-commit transaction by compare-and-set. These spans
// let an operator see each leg beside the postgres.query child spans that
// carry the statement latency. Attributes carry counts only (tuples,
// outcomes, slots, rows), never advisory ids or package names.
const (
	// SpanChangedNamesRead wraps one changed_name fact read, from the
	// statement through row scanning. Attributes carry counts only
	// (facts, advisories), never advisory ids or package names.
	SpanChangedNamesRead = "changed_names.read"

	// SpanOSVDeltaReplanDrain wraps one replan drain pass, from the pending
	// read through the slot-bounded plan and the drain commit. Attributes
	// carry counts only (pending, covered, overflow, slots), never advisory
	// ids or package names.
	SpanOSVDeltaReplanDrain = "osv_delta_replan.drain"
)
