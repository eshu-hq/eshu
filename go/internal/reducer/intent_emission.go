// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// ProjectionContext holds the bounded-unit freshness context for one shared
// projection repository slice.
type ProjectionContext struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
}

// copyPayload forwards to [payloadcore.CopyPayload].
func copyPayload(m map[string]any) map[string]any {
	return payloadcore.CopyPayload(m)
}

func (c ProjectionContext) acceptanceUnitID(repositoryID string) string {
	if unitID := strings.TrimSpace(c.AcceptanceUnitID); unitID != "" {
		return unitID
	}
	return strings.TrimSpace(repositoryID)
}
