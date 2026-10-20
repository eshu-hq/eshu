// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import "time"

// unixSeconds converts a License Manager Unix-seconds timestamp (used for the
// license-configuration expiry) into a UTC time. License Manager reports expiry
// as an integer Unix timestamp rather than an SDK *time.Time.
func unixSeconds(seconds int64) time.Time {
	return time.Unix(seconds, 0).UTC()
}
