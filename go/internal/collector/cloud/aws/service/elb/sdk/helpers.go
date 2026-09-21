// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

// cloneStrings returns a defensive copy of input, or nil when input is empty, so
// scanner-owned records do not alias AWS SDK response slices.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
