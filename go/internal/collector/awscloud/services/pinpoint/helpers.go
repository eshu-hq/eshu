// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pinpoint

import (
	"strings"
	"time"
)

// applicationResourceID returns the resource_id the application node publishes.
// It prefers the Pinpoint application id (the stable console Project ID) and
// falls back to the application ARN, then the name, so application-scoped edges
// can key the application by the same value the node publishes.
func applicationResourceID(application Application) string {
	return firstNonEmpty(application.ID, application.ARN, application.Name)
}

// segmentResourceID returns the resource_id the segment node publishes. It
// prefers the segment ARN and falls back to the application-qualified segment
// id, so a segment's own edges are sourced on the same id the segment node
// publishes.
func segmentResourceID(segment Segment) string {
	arn := strings.TrimSpace(segment.ARN)
	if arn != "" {
		return arn
	}
	app := strings.TrimSpace(segment.ApplicationID)
	id := strings.TrimSpace(segment.ID)
	switch {
	case app != "" && id != "":
		return app + "/" + id
	default:
		return id
	}
}

// channelResourceID returns the resource_id a channel-settings node publishes.
// A Pinpoint channel has no AWS-assigned ARN, so the scanner keys it by the
// stable application-id/channel-type pair, which is unique within a claim.
func channelResourceID(channel Channel) string {
	app := strings.TrimSpace(channel.ApplicationID)
	kind := strings.TrimSpace(channel.ChannelType)
	switch {
	case app != "" && kind != "":
		return app + "/" + kind
	case kind != "":
		return kind
	default:
		return app
	}
}

// sesIdentityNameFromARN extracts the SES email-identity name (the verified
// email address or domain) from an SES identity ARN of the form
// arn:<partition>:ses:<region>:<account>:identity/<name>. The SES scanner
// publishes its email-identity resource_id as that bare name, so the Pinpoint
// email-channel edge must key the same value to join. It returns "" when value
// is not an SES identity ARN, so the caller skips the edge rather than dangle a
// guess.
func sesIdentityNameFromARN(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "arn:") {
		return ""
	}
	marker := ":identity/"
	idx := strings.Index(value, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(value[idx+len(marker):])
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
