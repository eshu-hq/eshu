// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import "testing"

// The lane that fills coreToolNames (#8759) deletes this test in the same PR.
func TestCoreToolNamesEmptyInScaffold(t *testing.T) {
	t.Parallel()
	if got := CoreToolNames(); len(got) != 0 {
		t.Fatalf("CoreToolNames() = %v, want empty until #8759 fills it", got)
	}
}
