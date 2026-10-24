// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "testing"

// TestKindFailureClassesNameFirstPartyKinds pins that every key of the
// per-kind map is a collector kind the platform ships, so a misspelled kind
// cannot leave its classes unreachable while the tests for other kinds pass.
func TestKindFailureClassesNameFirstPartyKinds(t *testing.T) {
	t.Parallel()
	for kind, classes := range kindFailureClasses {
		if _, ok := firstPartyKinds[kind]; !ok {
			t.Errorf("kindFailureClasses key %q is not a first-party collector kind", kind)
		}
		if len(classes) == 0 {
			t.Errorf("kindFailureClasses[%q] is empty, want classes or no entry", kind)
		}
	}
}
