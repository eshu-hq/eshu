// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cleanrooms

import (
	"strings"
	"time"
)

// collaborationResourceID returns the resource_id a collaboration node
// publishes. It prefers the collaboration ARN (always present from the Clean
// Rooms API) and falls back to the collaboration id, so membership edges can key
// the collaboration by the same value the node publishes.
func collaborationResourceID(collaboration Collaboration) string {
	return firstNonEmpty(collaboration.ARN, collaboration.ID)
}

// configuredTableResourceID returns the resource_id a configured-table node
// publishes. It prefers the table ARN and falls back to the configured-table
// id, so the table's own edges are sourced on the same id the node publishes.
func configuredTableResourceID(table ConfiguredTable) string {
	return firstNonEmpty(table.ARN, table.ID)
}

// membershipResourceID returns the resource_id a membership node publishes. It
// prefers the membership ARN and falls back to the membership id.
func membershipResourceID(membership Membership) string {
	return firstNonEmpty(membership.ARN, membership.ID)
}

// glueTableResourceID returns the resource_id the Glue scanner publishes for a
// Glue table node: "<database>/<table>", or just "<table>" when no database is
// reported. It returns "" when no table name is present so a Glue edge is
// skipped rather than dangled.
func glueTableResourceID(databaseName, tableName string) string {
	databaseName = strings.TrimSpace(databaseName)
	tableName = strings.TrimSpace(tableName)
	switch {
	case databaseName != "" && tableName != "":
		return databaseName + "/" + tableName
	case tableName != "":
		return tableName
	default:
		return ""
	}
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
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

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
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
