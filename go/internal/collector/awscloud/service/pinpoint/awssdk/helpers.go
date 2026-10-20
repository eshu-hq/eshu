// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"
)

// channelTypeEmail is the Pinpoint channel-type key for the email channel in
// the GetChannels map response. The email channel is the only channel the
// adapter enriches with SES configuration-set and identity references.
const channelTypeEmail = "EMAIL"

// parseISO8601 parses a Pinpoint ISO 8601 timestamp string into a time.Time. It
// returns the zero time when value is empty or unparseable so the scanner omits
// an unknown timestamp instead of emitting an epoch.
func parseISO8601(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
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

// cloneTags returns a trimmed-key copy of the AWS tag map, or nil when it is
// empty or every key trims to empty.
func cloneTags(input map[string]string) map[string]string {
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
