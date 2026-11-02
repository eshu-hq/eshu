// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Stored advisory package names (#9777). The change-driven re-query path
// unions the names a changed OSV record carries now with the names Eshu already
// stores for the advisory id; this span lets an operator see how many ids a
// pass asked about and how many names came back, beside the postgres.query
// child spans that carry the statement latency.
const (
	// SpanStoredAdvisoryNamesRead wraps one stored-name read from the first
	// chunk statement through row scanning. Attributes carry counts only
	// (ids_requested, chunks, ids_resolved, packages), never advisory ids or
	// package names.
	SpanStoredAdvisoryNamesRead = "stored_advisory_names.read"
)
