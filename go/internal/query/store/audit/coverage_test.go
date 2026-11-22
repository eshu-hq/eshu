// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/store/audit"
)

// Seeded RED/GREEN pairs for the coverage ratchet: each RED case plants one
// failure and requires a problem naming it; the GREEN case is the clean list.
func TestCheckWaiversRatchet(t *testing.T) {
	t.Parallel()
	required := []string{"a", "b", "c"}
	covered := []string{"a"}
	if got := audit.CheckWaivers("capability", required, covered, []string{"b", "c"}, 2); len(got) != 0 {
		t.Fatalf("GREEN: a clean list reported %v", got)
	}
	for name, tc := range map[string]struct {
		required, covered, waived []string
		ceiling                   int
		want                      string
	}{
		"planted unprobed capability":         {[]string{"a", "b", "c", "d"}, []string{"a"}, []string{"b", "c"}, 2, `"d" has no coverage and no waiver`},
		"planted extra waiver":                {required, covered, []string{"b", "c", "a"}, 2, "exceed the ceiling 2"},
		"shrink without lowering the ceiling": {required, []string{"a", "b"}, []string{"c"}, 2, "lower the ceiling constant to 1"},
		"waiver for a covered capability":     {required, []string{"a", "b"}, []string{"b", "c"}, 2, "now has coverage"},
		"waiver for a missing capability":     {required, covered, []string{"b", "c", "gone"}, 3, `"gone", which does not exist`},
		"duplicate waiver":                    {required, covered, []string{"b", "c", "c"}, 3, "listed twice"},
	} {
		got := audit.CheckWaivers("capability", tc.required, tc.covered, tc.waived, tc.ceiling)
		found := false
		for _, problem := range got {
			if strings.Contains(problem, tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s RED: want a problem containing %q, got %v", name, tc.want, got)
		}
	}
}
