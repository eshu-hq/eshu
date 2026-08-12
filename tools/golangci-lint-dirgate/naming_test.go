// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestDetectNamingViolations(t *testing.T) {
	cases := []struct {
		name    string
		files   []string
		subpkgs []string
		want    []namingViolation
	}{
		{
			name:    "exact stem match belongs in the subpackage",
			files:   []string{"bar.go"},
			subpkgs: []string{"bar"},
			want:    []namingViolation{{File: "bar.go", Subpackage: "bar"}},
		},
		{
			name:    "underscore-prefixed stem belongs in the subpackage",
			files:   []string{"bar_baz.go"},
			subpkgs: []string{"bar"},
			want:    []namingViolation{{File: "bar_baz.go", Subpackage: "bar"}},
		},
		{
			name:    "prefix without an underscore boundary is not a violation",
			files:   []string{"barnacle.go"},
			subpkgs: []string{"bar"},
			want:    nil,
		},
		{
			name:    "unrelated file is not a violation",
			files:   []string{"unrelated.go"},
			subpkgs: []string{"bar"},
			want:    nil,
		},
		{
			name:    "no subpackages means no violations",
			files:   []string{"bar_baz.go"},
			subpkgs: nil,
			want:    nil,
		},
		{
			name:    "longer subpackage name is not defeated by a shorter false match",
			files:   []string{"awscloud_scanner.go"},
			subpkgs: []string{"aws"},
			want:    nil,
		},
		{
			name:    "each qualifying file is checked against every subpackage",
			files:   []string{"c_language.go", "unrelated.go", "cpp_language.go"},
			subpkgs: []string{"c", "cpp"},
			want: []namingViolation{
				{File: "c_language.go", Subpackage: "c"},
				{File: "cpp_language.go", Subpackage: "cpp"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectNamingViolations(c.files, c.subpkgs)
			if !equalNamingViolations(got, c.want) {
				t.Fatalf("detectNamingViolations(%v, %v) = %v, want %v", c.files, c.subpkgs, got, c.want)
			}
		})
	}
}

func equalNamingViolations(a, b []namingViolation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
