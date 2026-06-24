// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securitylake

import (
	"strings"
	"time"
)

// dataLakeResourceID returns the resource_id the data lake node publishes. It
// prefers the data lake ARN (always present from the Security Lake API) and
// falls back to a region-scoped synthetic id so log-source edges can key the
// data lake by the same value the node publishes.
func dataLakeResourceID(lake DataLake) string {
	if arn := strings.TrimSpace(lake.ARN); arn != "" {
		return arn
	}
	region := strings.TrimSpace(lake.Region)
	if region == "" {
		return ""
	}
	return "securitylake:" + region
}

// logSourceResourceID returns the resource_id a log-source node publishes. A log
// source has no AWS ARN, so the scanner builds a stable composite id from the
// collection scope (account, region, source name, custom flag) that uniquely
// identifies the source within the account.
func logSourceResourceID(source LogSource) string {
	parts := []string{
		strings.TrimSpace(source.Account),
		strings.TrimSpace(source.Region),
		sourceKind(source.Custom),
		strings.TrimSpace(source.SourceName),
	}
	if version := strings.TrimSpace(source.SourceVersion); version != "" {
		parts = append(parts, version)
	}
	id := strings.Join(nonEmpty(parts), "/")
	if id == "" {
		return ""
	}
	return "securitylake-source:" + id
}

// subscriberResourceID returns the resource_id a subscriber node publishes. It
// prefers the subscriber ARN and falls back to the subscriber id.
func subscriberResourceID(subscriber Subscriber) string {
	return firstNonEmpty(subscriber.ARN, subscriber.ID)
}

// sourceKind returns the stable scope token for a log source kind so the
// composite resource_id distinguishes an AWS-native source from a third-party
// custom source with the same name.
func sourceKind(custom bool) string {
	if custom {
		return "custom"
	}
	return "aws"
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

// nonEmpty returns a copy of values with trimmed-empty entries dropped.
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
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
