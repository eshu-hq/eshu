// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package autoscaling

import "time"

// timeOrNil returns the UTC time or nil so empty timestamps stay absent from
// fact payloads instead of serializing the zero value.
func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

// int32PtrOrNil returns the dereferenced value or nil so an unset optional
// scheduled-action capacity stays absent from the fact payload instead of
// serializing a misleading zero.
func int32PtrOrNil(input *int32) any {
	if input == nil {
		return nil
	}
	return *input
}
