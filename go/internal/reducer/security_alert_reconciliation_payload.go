// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

// parseSecurityAlertTime parses an RFC 3339 timestamp string into a time.Time,
// returning the zero time for an empty or unparseable value. The reducer uses
// the parsed provider updated_at to decide whether owned dependency evidence is
// newer than the provider alert.
func parseSecurityAlertTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
