// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package licensemanager

import (
	"sort"
	"strings"
	"time"
)

// sortStrings sorts input in place in ascending order, keeping the metadata
// payload deterministic across scans.
func sortStrings(input []string) {
	sort.Strings(input)
}

// configurationResourceID returns the resource_id the configuration node
// publishes. It prefers the configuration ARN (always present from the License
// Manager API) and falls back to the configuration id, then the name, so a
// configuration's own edges are sourced on the same id the node publishes.
func configurationResourceID(configuration Configuration) string {
	return firstNonEmpty(configuration.ARN, configuration.ID, configuration.Name)
}

// instanceIDFromARN extracts the bare EC2 instance id (i-...) from an EC2
// instance ARN of the form arn:<partition>:ec2:<region>:<account>:instance/i-...
// It returns "" when value is not an instance ARN, so a non-instance or
// malformed ARN never keys a dangling edge. The function never synthesizes an
// ARN; it only reads the trailing instance id the EC2 instance node publishes.
func instanceIDFromARN(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return ""
	}
	idx := strings.LastIndex(trimmed, ":instance/")
	if idx < 0 {
		return ""
	}
	id := strings.TrimSpace(trimmed[idx+len(":instance/"):])
	if !strings.HasPrefix(id, "i-") {
		return ""
	}
	return id
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
