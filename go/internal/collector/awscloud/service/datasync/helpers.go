// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datasync

import (
	"strings"
	"time"
)

// trimLogGroupWildcardARN removes a trailing `:*` wildcard from a CloudWatch
// log group ARN so the join key matches the canonical log-group ARN the
// CloudWatch Logs scanner publishes as its resource_id.
func trimLogGroupWildcardARN(arn string) string {
	return strings.TrimSuffix(strings.TrimSpace(arn), ":*")
}

// isARN reports whether value has the canonical AWS ARN scheme prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// timeOrNil returns the UTC time value, or nil when the time is the zero value,
// so an absent timestamp is omitted from the fact payload rather than recorded
// as the Unix epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
