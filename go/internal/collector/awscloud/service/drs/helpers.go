// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drs

import (
	"strings"
)

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// sourceServerResourceID returns the resource_id the source server node
// publishes. It prefers the source server id (always present from the DRS API)
// and falls back to the ARN, so a source server's own edges are sourced on the
// same id the node publishes.
func sourceServerResourceID(server SourceServer) string {
	return firstNonEmpty(server.SourceServerID, server.ARN)
}

// recoveryInstanceResourceID returns the resource_id the recovery instance node
// publishes. It prefers the recovery instance id and falls back to the ARN.
func recoveryInstanceResourceID(instance RecoveryInstance) string {
	return firstNonEmpty(instance.RecoveryInstanceID, instance.ARN)
}

// templateResourceID returns the resource_id the replication configuration
// template node publishes. It prefers the template id and falls back to the ARN.
func templateResourceID(template ReplicationConfigurationTemplate) string {
	return firstNonEmpty(template.TemplateID, template.ARN)
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

// stringOrNil returns the trimmed value when non-empty, or nil so the attribute
// payload omits an unknown field instead of emitting an empty string.
func stringOrNil(value string) any {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return nil
}
