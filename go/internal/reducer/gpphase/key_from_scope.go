// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import "strings"

// KeyFromScope builds the bounded readiness identity for one scope
// generation and keyspace. The acceptance unit id is the first non-blank
// entry in entityKeys, falling back to scopeID when every entity key is
// blank. That derivation is [AcceptanceUnitID], which the reducer root phase
// publisher and every domain family share before publishing or reading a
// readiness phase, so a family's lookup key always matches the key the
// publishing family constructed. It returns
// (key, false) when scopeID or generationID is blank.
func KeyFromScope(scopeID, generationID string, entityKeys []string, keyspace Keyspace) (PhaseKey, bool) {
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" || generationID == "" {
		return PhaseKey{}, false
	}
	acceptanceUnitID := AcceptanceUnitID(scopeID, entityKeys)
	return PhaseKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         keyspace,
	}, true
}
