// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lightsail

import (
	"strings"
	"time"
)

// timeOrNil returns value in UTC, or nil when it is the zero time, so optional
// timestamp attributes stay absent rather than serializing a zero value.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// int32OrNil returns the pointed-to int32, or nil when value is nil, so optional
// numeric attributes stay absent rather than serializing a zero value.
func int32OrNil(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

// cloneStringSlice returns a trimmed copy of input with blank entries dropped,
// or nil when nothing survives, keeping the omitempty-style payload behavior
// consistent across services.
func cloneStringSlice(input []string) []string {
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

// cloneInt32Slice returns a copy of input, or nil when input is empty, so
// optional numeric-list attributes stay absent rather than serializing an empty
// slice.
func cloneInt32Slice(input []int32) []int32 {
	if len(input) == 0 {
		return nil
	}
	output := make([]int32, len(input))
	copy(output, input)
	return output
}
