// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

import (
	"strings"
	"testing"
)

// TestBoundedFailureClassLabel keeps a failure_class metric label bounded
// (#7386): a lowercase identifier passes, anything else is "other".
func TestBoundedFailureClassLabel(t *testing.T) {
	t.Parallel()

	for class, want := range map[string]string{
		"graph_write_timeout":       "graph_write_timeout",
		"retry_exhausted":           "retry_exhausted",
		strings.Repeat("a", 64):     strings.Repeat("a", 64),
		strings.Repeat("a", 65):     FailureClassOtherLabel,
		"":                          FailureClassOtherLabel,
		"Weird/Class:1":             FailureClassOtherLabel,
		"Projection_Bug":            FailureClassOtherLabel,
		"with space":                FailureClassOtherLabel,
		"trailing_newline_is_bad\n": FailureClassOtherLabel,
	} {
		if got := BoundedFailureClassLabel(class); got != want {
			t.Errorf("BoundedFailureClassLabel(%.20q) = %.20q, want %.20q", class, got, want)
		}
	}
}
