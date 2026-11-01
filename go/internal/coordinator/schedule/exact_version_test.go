// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import "testing"

func TestExactOwnedDependencyVersionForEcosystemIsPerEcosystem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ecosystem, raw, want string
		ok                   bool
	}{
		{"npm", " 1.2.3 ", "1.2.3", true},
		{"npm", "^1.0.0", "", false},
		{"npm", "latest", "", false},
		{"npm", "latest.release", "", false},
		{"npm", "main", "", false},
		{"maven", "[1.0,2.0)", "", false},
		{"maven", "1.0-SNAPSHOT", "", false},
		{"maven", "1.0@jar", "", false},
		{"maven", "${version}", "", false},
		{"go", "v1.2.3", "v1.2.3", true},
		{"go", "1.2.3", "", false},
		{"go", "vbad", "", false},
		{"pypi", "1!2.0.0", "1!2.0.0", true},
		{"maven", "1.0!!", "", false},
		{"npm", "", "", false},
	} {
		got, ok := ExactOwnedDependencyVersionForEcosystem(tc.ecosystem, tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ExactOwnedDependencyVersionForEcosystem(%q, %q) = (%q, %v), want (%q, %v)",
				tc.ecosystem, tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}
