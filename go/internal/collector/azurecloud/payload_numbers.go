// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import "fmt"

const maxInt32PayloadValue = 1<<31 - 1

func nonNegativeInt32PayloadValue(field string, value int64) (int32, error) {
	if value < 0 || value > maxInt32PayloadValue {
		return 0, fmt.Errorf("%s must fit non-negative int32: %d", field, value)
	}
	// #nosec G115 -- value is range-checked above before narrowing to the schema type.
	return int32(value), nil
}

func nonNegativeInt32PayloadCount(field string, count int) (int32, error) {
	if count < 0 || count > maxInt32PayloadValue {
		return 0, fmt.Errorf("%s must fit non-negative int32: %d", field, count)
	}
	// #nosec G115 -- count is range-checked above before narrowing to the schema type.
	return int32(count), nil
}
