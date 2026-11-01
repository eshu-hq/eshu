// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Owned-package name resolver (#9777). The change-driven re-query path maps
// the OSV package names a feed change touched onto the owned dependency
// inventory; this span lets an operator see how many names a pass resolved,
// how many statements it ran, and how many lookups were bare swift names whose
// match cannot be proven complete, beside the postgres.query child spans that
// carry the statement latency.
const (
	// SpanOwnedPackageResolve wraps one name-resolution call from the first
	// statement through row matching. Attributes carry counts only (lookups,
	// ecosystems, folded names, statements, rows_read, targets, partial),
	// never package names, on the span or on an error it records.
	SpanOwnedPackageResolve = "owned_package_targets.resolve"
)
