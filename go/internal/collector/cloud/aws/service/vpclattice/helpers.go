// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpclattice

import (
	"strings"
	"time"
)

// serviceNetworkResourceID returns the resource_id the service network node
// publishes. It prefers the ARN (always present from the API) and falls back to
// the id, so service-network edges key on the same value the node publishes.
func serviceNetworkResourceID(network ServiceNetwork) string {
	return firstNonEmpty(network.ARN, network.ID)
}

// serviceResourceID returns the resource_id the service node publishes. It
// prefers the ARN and falls back to the id.
func serviceResourceID(service Service) string {
	return firstNonEmpty(service.ARN, service.ID)
}

// targetGroupResourceID returns the resource_id the target group node
// publishes. It prefers the ARN and falls back to the id.
func targetGroupResourceID(group TargetGroup) string {
	return firstNonEmpty(group.ARN, group.ID)
}

// listenerResourceID returns the resource_id the listener node publishes. It
// prefers the ARN and falls back to the id.
func listenerResourceID(listener Listener) string {
	return firstNonEmpty(listener.ARN, listener.ID)
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// isInstanceID reports whether value is a bare EC2 instance id (i-...), the
// resource_id form the load-balancer-to-instance and target-to-instance edges
// key on.
func isInstanceID(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "i-")
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
