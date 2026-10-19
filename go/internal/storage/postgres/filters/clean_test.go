// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package filters

import "testing"

// TestCleanFactKindsIsSingleSource is the seeded RED for #8552: the postgres
// root and the tfstate subpackage each carry their own cleanFactKinds copy
// today, identical by prose alone. This package is the one importable source;
// the test pins trim/dedup/order so a mutation that changes the copy breaks
// here instead of drifting silently.
func TestCleanFactKindsIsSingleSource(t *testing.T) {
	got := CleanFactKinds([]string{"", "  b ", "a", "b", "  ", "a", "c"})
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("CleanFactKinds() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CleanFactKinds() = %v, want %v", got, want)
		}
	}
}
