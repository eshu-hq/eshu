// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadid

import (
	"testing"
)

// TestClassifyWorkloadSelectorLadder is the T5 handle-ladder table for the
// selector vocabulary: a full scoped id, a public workload: handle, and
// anything else callers may type. The query and reducer ladders branch on
// this classification, so a selector that is neither must never read as a
// handle.
func TestClassifyWorkloadSelectorLadder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		selector string
		want     SelectorKind
	}{
		{"exact scoped id", "workload:repository:r_8477a002:checkout", SelectorKindFullID},
		{"scoped id with surrounding space", "  workload:repository:r_8477a002:checkout  ", SelectorKindFullID},
		{"unique handle", "workload:checkout", SelectorKindHandle},
		{"handle with surrounding space", "  workload:checkout  ", SelectorKindHandle},
		{"bare name", "checkout", SelectorKindBare},
		{"bare name with surrounding space", "  checkout  ", SelectorKindBare},
		{"empty", "", SelectorKindBare},
		{"blank", "   ", SelectorKindBare},
		{"bare prefix is not a handle", "workload:", SelectorKindBare},
		{"other scheme is not a handle", "service:checkout", SelectorKindBare},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyWorkloadSelector(tc.selector); got != tc.want {
				t.Fatalf("ClassifyWorkloadSelector(%q) = %q, want %q", tc.selector, got, tc.want)
			}
		})
	}
}

// TestStripWorkloadHandlePrefixLadder pins the rung-2 derivation: only a
// handle loses its prefix, and the bare name is trimmed. A full scoped id is
// returned unchanged so a caller that strips without classifying cannot turn
// an id into a phantom name.
func TestStripWorkloadHandlePrefixLadder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		selector string
		want     string
	}{
		{"handle strips to bare", "workload:checkout", "checkout"},
		{"handle with space strips to bare", "  workload:checkout  ", "checkout"},
		{"bare is unchanged", "checkout", "checkout"},
		{"full id is unchanged", "workload:repository:r_8477a002:checkout", "workload:repository:r_8477a002:checkout"},
		{"blank is empty", "   ", ""},
		{"bare prefix is empty", "workload:", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := StripWorkloadHandlePrefix(tc.selector); got != tc.want {
				t.Fatalf("StripWorkloadHandlePrefix(%q) = %q, want %q", tc.selector, got, tc.want)
			}
		})
	}
}

// TestFormatWorkloadHandleLadder pins the public handle shape the identity
// fallbacks emit beside the node id: one prefix, trimmed, never blank.
func TestFormatWorkloadHandleLadder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain name", "checkout", "workload:checkout"},
		{"trimmed name", "  checkout  ", "workload:checkout"},
		{"already prefixed stays single", "workload:checkout", "workload:checkout"},
		{"blank is empty", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatWorkloadHandle(tc.in); got != tc.want {
				t.Fatalf("FormatWorkloadHandle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
