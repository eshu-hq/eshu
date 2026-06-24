// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package signer

import "strings"

// profileResourceID returns the resource_id the signing-profile node publishes.
// It prefers the profile ARN (always present from the Signer API) and falls back
// to the profile name, so a profile's own edges are sourced on the same id the
// profile node publishes.
func profileResourceID(profile SigningProfile) string {
	return firstNonEmpty(profile.ARN, profile.Name)
}

// platformResourceID returns the resource_id the signing-platform node
// publishes: the bare platform id (for example AWSLambda-SHA384-ECDSA). Signer
// platforms carry no AWS ARN, so the bare id is the stable join key for the
// profile-to-platform internal edge.
func platformResourceID(platform SigningPlatform) string {
	return strings.TrimSpace(platform.PlatformID)
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
