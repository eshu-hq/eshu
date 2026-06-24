// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package batch

import "time"

// timeOrNil returns the UTC time or nil so empty timestamps stay absent from
// fact payloads instead of serializing the zero value.
func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
