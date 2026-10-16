// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import (
	"fmt"
	"slices"
	"testing"
)

// TestNormalizeStreamDroppedPaths (#8185): the recorded drop set is trimmed,
// deduplicated, sorted, and capped, so two runs that dropped the same files
// produce the same record.
func TestNormalizeStreamDroppedPaths(t *testing.T) {
	got := NormalizeStreamDroppedPaths([]string{"b.py", "  ", "a.py", "b.py", "", "c.py"})
	if want := []string{"a.py", "b.py", "c.py"}; !slices.Equal(got, want) {
		t.Fatalf("normalized = %q, want %q", got, want)
	}
	many := make([]string, 0, MaxRecordedStreamDroppedPaths+5)
	for i := 0; i < MaxRecordedStreamDroppedPaths+5; i++ {
		many = append(many, fmt.Sprintf("f%03d.py", i))
	}
	if got := NormalizeStreamDroppedPaths(many); len(got) != MaxRecordedStreamDroppedPaths {
		t.Fatalf("capped count = %d, want %d", len(got), MaxRecordedStreamDroppedPaths)
	}
	if got := NormalizeStreamDroppedPaths(nil); got != nil {
		t.Fatalf("empty normalized = %q, want nil", got)
	}
}

// TestStreamDroppedPathsTruncated (#8185): an overflow reports truncated so
// the skip probe refuses; an exact-cap or smaller set does not.
func TestStreamDroppedPathsTruncated(t *testing.T) {
	var nilSet *StreamDroppedPaths
	if nilSet.Truncated() {
		t.Fatal("nil set truncated = true, want false")
	}
	under := new(StreamDroppedPaths)
	under.Note("a.py")
	if under.Truncated() {
		t.Fatal("one-path set truncated = true, want false")
	}
	over := new(StreamDroppedPaths)
	for i := 0; i < MaxRecordedStreamDroppedPaths+1; i++ {
		over.Note(fmt.Sprintf("f%03d.py", i))
	}
	if !over.Truncated() {
		t.Fatal("over-cap set truncated = false, want true")
	}
	if got := over.Sorted(); len(got) != MaxRecordedStreamDroppedPaths {
		t.Fatalf("over-cap sorted count = %d, want %d", len(got), MaxRecordedStreamDroppedPaths)
	}
}
