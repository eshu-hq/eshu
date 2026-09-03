// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

func deadCodeIsCandidateEntity(result map[string]any, entity *EntityContent) bool {
	for _, label := range StringSliceVal(result, "labels") {
		if deadCodeIsCandidateEntityType(label) {
			return true
		}
	}
	if entity == nil {
		return false
	}
	return deadCodeIsCandidateEntityType(entity.EntityType)
}

func deadCodeIsCandidateEntityType(entityType string) bool {
	switch strings.TrimSpace(entityType) {
	case "Function", "Class", "Struct", "Interface", "Trait", "SqlFunction":
		return true
	default:
		return false
	}
}

// isDeadCodeCandidateLabel reports whether label is one of the graph node
// labels the candidate scan may target. It guards every label-interpolated
// candidate query, so an unrecognised label falls back to Function rather than
// rendering caller text into Cypher.
func isDeadCodeCandidateLabel(label string) bool {
	for _, candidate := range deadCodeCandidateLabels {
		if label == candidate {
			return true
		}
	}
	return false
}
