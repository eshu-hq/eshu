// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

// resolvedIDSet duplicates the reconciliation test helper from package cypher
// (generation_reconciliation_test.go). Test helpers cannot be imported across
// the package split, so each side carries its own copy.
func resolvedIDSet(ids ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}
