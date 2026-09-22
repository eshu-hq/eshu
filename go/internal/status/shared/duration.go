// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "time"

// NonNegativeDuration normalizes status-age fields that can briefly go
// negative when database timestamps are newer than the status read clock.
func NonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}
