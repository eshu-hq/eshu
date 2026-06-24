// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package timestream

import (
	"strings"
	"time"
)

// databaseResourceID returns the resource_id the database node publishes. It
// prefers the database ARN (always present from the Timestream API) and falls
// back to the database name, so table-in-database edges can key the database by
// the same value the node publishes.
func databaseResourceID(database Database) string {
	return firstNonEmpty(database.ARN, database.Name)
}

// tableResourceID returns the resource_id the table node publishes. It prefers
// the table ARN and falls back to the qualified database/table name, so a
// table's own edges are sourced on the same id the table node publishes.
func tableResourceID(table Table) string {
	arn := strings.TrimSpace(table.ARN)
	if arn != "" {
		return arn
	}
	database := strings.TrimSpace(table.DatabaseName)
	name := strings.TrimSpace(table.Name)
	switch {
	case database != "" && name != "":
		return database + "/" + name
	default:
		return name
	}
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN for name, or
// returns an already-formed ARN unchanged. S3 buckets have no API ARN, so the
// scanner synthesizes one carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the table->bucket target matches the S3 scanner's published
// bucket resource_id in every partition instead of dangling the graph edge.
func arnForBucket(partition, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "arn:") {
		return name
	}
	return "arn:" + partition + ":s3:::" + name
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
