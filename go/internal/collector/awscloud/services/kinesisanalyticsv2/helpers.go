// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesisanalyticsv2

import (
	"strings"
	"time"
)

// applicationResourceID returns the resource_id the application node publishes.
// It prefers the application ARN (always present from the API) and falls back to
// the application name, so an application's own edges are sourced on the same id
// the application node publishes.
func applicationResourceID(application Application) string {
	return firstNonEmpty(application.ARN, application.Name)
}

// snapshotNames returns the trimmed, distinct snapshot names in input order, or
// nil when none survive. Only snapshot names are surfaced; no snapshot data or
// persisted application state is read.
func snapshotNames(snapshots []Snapshot) []string {
	if len(snapshots) == 0 {
		return nil
	}
	names := make([]string, 0, len(snapshots))
	seen := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		name := strings.TrimSpace(snapshot.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
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
