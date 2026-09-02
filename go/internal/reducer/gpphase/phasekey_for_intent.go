// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import "strings"

// AcceptanceUnitID derives the bounded acceptance-unit identity a reducer
// intent's readiness key is keyed on: the intent's first non-blank entity key,
// or the scope id when the intent carries none. The entity key wins because a
// readiness fact is per-slice, not per-scope: a scope that projects several
// acceptance units publishes one readiness row per unit, and collapsing them
// onto the scope id would let one unit's commit open the gate for another.
//
// It takes the two fields rather than a reducer intent so this package stays a
// standard-library-only leaf that the reducer root and its family packages can
// both reach.
func AcceptanceUnitID(scopeID string, entityKeys []string) string {
	for _, entityKey := range entityKeys {
		if trimmed := strings.TrimSpace(entityKey); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(scopeID)
}

// PhaseKeyForIntent builds the readiness key one reducer intent gates a graph
// write on. It reports false when the intent cannot name a bounded readiness
// slice — a blank scope id, a blank generation id, or an acceptance unit that
// resolves to nothing — so a caller gates closed rather than reading readiness
// under a partly-blank key that [PhaseKey.Validate] would reject.
//
// The generation id doubles as the source run id: a reducer generation is the
// run that publishes the readiness fact.
func PhaseKeyForIntent(scopeID, generationID string, entityKeys []string, keyspace Keyspace) (PhaseKey, bool) {
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" || generationID == "" {
		return PhaseKey{}, false
	}

	acceptanceUnitID := AcceptanceUnitID(scopeID, entityKeys)
	if acceptanceUnitID == "" {
		return PhaseKey{}, false
	}

	return PhaseKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         keyspace,
	}, true
}
