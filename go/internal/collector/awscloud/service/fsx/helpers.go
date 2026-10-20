// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fsx

import "strings"

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

func int32OrNil(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

func int64OrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// relationshipRecordID encodes the relationship type into the durable
// SourceRecordID alongside the source and final target identity. Including the
// relationship type keeps each relationship envelope's source ref distinct when
// a source has multiple edges to the same target and stays stable when the
// final target identity is upgraded from a raw ID to an ARN.
func relationshipRecordID(sourceID, relationshipType, targetID string) string {
	return strings.TrimSpace(sourceID) + "->" + strings.TrimSpace(relationshipType) + ":" + strings.TrimSpace(targetID)
}
