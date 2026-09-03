// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import "strings"

// ProjectionContext holds the bounded-unit freshness context for one shared
// projection repository slice. It is the identity half of [Input]: a materializer
// builds one context per repository it loaded facts for, then stamps it onto
// every intent that repository produces.
type ProjectionContext struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
}

// ResolveAcceptanceUnitID returns the acceptance unit the context names,
// falling back to the repository ID when the context carries none. [Build]
// applies the same fallback, so a caller that reads the value back (to key a
// map, or to build a partition key) sees exactly what the stored row will
// carry. The method is not named for the field it reads because a Go struct
// cannot carry a field and a method under one name.
func (c ProjectionContext) ResolveAcceptanceUnitID(repositoryID string) string {
	if unitID := strings.TrimSpace(c.AcceptanceUnitID); unitID != "" {
		return unitID
	}
	return strings.TrimSpace(repositoryID)
}
