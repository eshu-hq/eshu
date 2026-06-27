// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

// ProjectionContext holds the bounded-unit freshness context for one shared
// projection repository slice.
type ProjectionContext struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
}

// copyPayload creates a shallow copy of the payload map so shared-projection
// intent builders can stamp per-domain fields without mutating the source row.
func copyPayload(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func (c ProjectionContext) acceptanceUnitID(repositoryID string) string {
	if unitID := strings.TrimSpace(c.AcceptanceUnitID); unitID != "" {
		return unitID
	}
	return strings.TrimSpace(repositoryID)
}
