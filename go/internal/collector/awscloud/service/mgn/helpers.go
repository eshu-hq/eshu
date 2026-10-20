// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mgn

import (
	"strings"
	"time"
)

// sourceServerResourceID returns the resource_id the source-server node
// publishes. It prefers the MGN source server id (always present from the API
// and the value applications and jobs reference) and falls back to the ARN, so
// internal edges key the source server by the same value the node publishes.
func sourceServerResourceID(server SourceServer) string {
	return firstNonEmpty(server.SourceServerID, server.ARN)
}

// applicationResourceID returns the resource_id the application node publishes.
// It prefers the MGN application id (the value source servers reference) and
// falls back to the ARN.
func applicationResourceID(application Application) string {
	return firstNonEmpty(application.ApplicationID, application.ARN)
}

// jobResourceID returns the resource_id the job node publishes. It prefers the
// MGN job id and falls back to the ARN.
func jobResourceID(job Job) string {
	return firstNonEmpty(job.JobID, job.ARN)
}

// launchConfigurationResourceID returns the resource_id the launch
// configuration node publishes. Launch configurations have no AWS ARN, so the
// scanner keys the node on a stable qualified id derived from the owning source
// server id, ensuring the launch-config node and its edges share one identity.
func launchConfigurationResourceID(sourceServerID string) string {
	sourceServerID = strings.TrimSpace(sourceServerID)
	if sourceServerID == "" {
		return ""
	}
	return sourceServerID + "/launch-configuration"
}

// isInstanceID reports whether value is a bare EC2 instance id (i-...). MGN
// reports the launched instance as a bare instance id, which is how the EC2
// instance family is keyed, so the scanner never synthesizes an ARN for it.
func isInstanceID(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "i-")
}

// isLaunchTemplateID reports whether value is a bare EC2 launch template id
// (lt-...), the form MGN reports and the launch-template family is keyed under.
func isLaunchTemplateID(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "lt-")
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
