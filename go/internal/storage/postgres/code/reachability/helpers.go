// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/codeintel"
)

func cleanCodeReachabilityEntityIDs(entityIDs []string) []string {
	seen := make(map[string]struct{}, len(entityIDs))
	cleaned := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		cleaned = append(cleaned, entityID)
	}
	return cleaned
}

func strongerCodeReachabilityRow(left, right codeintel.CodeReachabilityRow) bool {
	if left.Confidence != right.Confidence {
		return left.Confidence > right.Confidence
	}
	if left.Depth != right.Depth {
		return left.Depth < right.Depth
	}
	return left.RootEntityID < right.RootEntityID
}
