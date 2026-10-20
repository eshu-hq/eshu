// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proton

import (
	"strings"
	"time"
)

// environmentResourceID returns the resource_id the environment node publishes.
// It prefers the environment ARN (always present from the Proton API) and falls
// back to the environment name, so service-in-environment edges can key the
// environment by the same value the node publishes.
func environmentResourceID(environment Environment) string {
	return firstNonEmpty(environment.ARN, environment.Name)
}

// serviceResourceID returns the resource_id the service node publishes. It
// prefers the service ARN and falls back to the service name.
func serviceResourceID(service Service) string {
	return firstNonEmpty(service.ARN, service.Name)
}

// templateResourceID returns the resource_id a template node publishes. It
// prefers the template ARN and falls back to the template name.
func templateResourceID(template Template) string {
	return firstNonEmpty(template.ARN, template.Name)
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
