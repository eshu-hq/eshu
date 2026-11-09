// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import "testing"

func TestGroupIDFromEntityKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want string
		ok   bool
	}{
		{EntityKey("CVE-2021-44228"), "CVE-2021-44228", true},
		{"advisory_group:GHSA-jfh8-c2jp-5v3q", "GHSA-jfh8-c2jp-5v3q", true},
		{"supply_chain_impact:scope-1", "", false},
		{"advisory_group:", "", false},
		{"advisory_group:  ", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got, ok := GroupIDFromEntityKey(tc.key)
		if got != tc.want || ok != tc.ok {
			t.Errorf("GroupIDFromEntityKey(%q) = %q, %v; want %q, %v", tc.key, got, ok, tc.want, tc.ok)
		}
	}
}
