// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import "testing"

// CoreToolNames hands out a copy, so a caller that edits the result cannot
// change what core_router advertises.
func TestCoreToolNamesReturnsCopy(t *testing.T) {
	t.Parallel()
	first := CoreToolNames()
	if len(first) == 0 {
		t.Fatal("CoreToolNames() is empty, want the measured core set (#8759)")
	}
	want := first[0]
	first[0] = "mutated"
	if got := CoreToolNames()[0]; got != want {
		t.Fatalf("CoreToolNames()[0] = %q after caller edit, want %q", got, want)
	}
}
