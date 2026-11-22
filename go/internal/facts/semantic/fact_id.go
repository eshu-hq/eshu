// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import "github.com/eshu-hq/eshu/go/internal/facts/encode"

// ScopedFactID returns the fact_records primary key for one semantic fact.
//
// fact_records is keyed on fact_id alone, so a fact id built from the stable
// key by itself is shared by every scope and generation that emits the same
// semantic identity, and a write from one of them lands on the other's row
// (issue #11203). The id therefore hashes the fact kind, the stable key, the
// scope, and the generation together. StableFactKey stays the semantic
// identity and is deliberately not scoped: it is what readers use to match one
// observation across generations. Hashing goes through encode.StableID, whose
// map encoding is length-delimited by JSON, so two different (scope,
// generation) pairs cannot alias by delimiter shifting.
func ScopedFactID(factKind, stableKey, scopeID, generationID string) string {
	return encode.StableID(factKind+".fact_id", map[string]any{
		"stable_fact_key": stableKey,
		"scope_id":        scopeID,
		"generation_id":   generationID,
	})
}
