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
// It takes the two fields rather than a reducer intent so this function
// itself needs no import beyond the standard library, even though the
// package as a whole no longer is standard-library-only: [StateForIntentValue]
// (issue #6061) added a dependency on `internal/reducer/contract` for the
// [reducercontract.Intent] value type. A family that wants the acceptance
// unit without pulling in that dependency can still call this function
// directly, the same way [KeyFromScope] and [IntentAnchor.AcceptanceUnitID]
// do.
func AcceptanceUnitID(scopeID string, entityKeys []string) string {
	for _, entityKey := range entityKeys {
		if trimmed := strings.TrimSpace(entityKey); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(scopeID)
}
