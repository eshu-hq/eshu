// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import "strings"

// KeyFromScope builds the bounded readiness identity for one scope
// generation and keyspace. The acceptance unit id is the first non-blank
// entry in entityKeys, falling back to scopeID when every entity key is
// blank — the same acceptance-unit derivation every domain family uses
// before publishing or reading a readiness phase, so a family's lookup key
// always matches the key the publishing family constructed. It returns
// (key, false) when scopeID or generationID is blank.
func KeyFromScope(scopeID, generationID string, entityKeys []string, keyspace Keyspace) (PhaseKey, bool) {
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" || generationID == "" {
		return PhaseKey{}, false
	}
	acceptanceUnitID := scopeID
	for _, entityKey := range entityKeys {
		if trimmed := strings.TrimSpace(entityKey); trimmed != "" {
			acceptanceUnitID = trimmed
			break
		}
	}
	return PhaseKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         keyspace,
	}, true
}
