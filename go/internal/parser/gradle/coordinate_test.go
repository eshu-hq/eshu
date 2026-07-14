// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import "testing"

// TestExtractMapEntryMatchesColonAndEqualsForms pins extractMapEntry's output
// for the two map-form dependency shapes (Groovy `group: 'x'` and Kotlin DSL
// `group = "x"`) plus the missing-key case, before and after the per-call
// regex compiles in extractMapEntry are hoisted to package-level vars. The
// hoist must not change which value is matched for the "group", "name", and
// "version" keys used by parseMapCoordinate.
func TestExtractMapEntryMatchesColonAndEqualsForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		key   string
		want  string
	}{
		{
			name:  "colon form double quoted",
			value: `group: "com.google.guava"`,
			key:   "group",
			want:  "com.google.guava",
		},
		{
			name:  "colon form single quoted",
			value: `group: 'com.google.guava', name: 'guava'`,
			key:   "name",
			want:  "guava",
		},
		{
			name:  "equals form kotlin dsl",
			value: `group = "org.jetbrains.kotlin", name = "kotlin-stdlib", version = "1.9.10"`,
			key:   "version",
			want:  "1.9.10",
		},
		{
			name:  "prefers colon form when both templates could match",
			value: `group: 'com.google.guava'`,
			key:   "group",
			want:  "com.google.guava",
		},
		{
			name:  "missing key returns empty",
			value: `group: 'com.google.guava', name: 'guava'`,
			key:   "version",
			want:  "",
		},
		{
			name:  "any key is matched generically, not restricted to group/name/version",
			value: `scope: 'com.google.guava'`,
			key:   "scope",
			want:  "com.google.guava",
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := extractMapEntry(testCase.value, testCase.key)
			if got != testCase.want {
				t.Fatalf("extractMapEntry(%q, %q) = %q, want %q", testCase.value, testCase.key, got, testCase.want)
			}
		})
	}
}

// TestParseMapCoordinateCombinesGroupNameVersion pins parseMapCoordinate's
// combined output across the fixed "group"/"name"/"version" key set that
// extractMapEntry is called with.
func TestParseMapCoordinateCombinesGroupNameVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  coordinate
		ok    bool
	}{
		{
			name:  "colon form with version",
			value: `group: 'com.google.guava', name: 'guava', version: '31.1-jre'`,
			want:  coordinate{group: "com.google.guava", artifact: "guava", version: "31.1-jre"},
			ok:    true,
		},
		{
			name:  "equals form without version",
			value: `group = "org.jetbrains.kotlin", name = "kotlin-stdlib"`,
			want:  coordinate{group: "org.jetbrains.kotlin", artifact: "kotlin-stdlib", version: ""},
			ok:    true,
		},
		{
			name:  "missing name is not a coordinate",
			value: `group: 'com.google.guava'`,
			want:  coordinate{},
			ok:    false,
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseMapCoordinate(testCase.value)
			if ok != testCase.ok {
				t.Fatalf("parseMapCoordinate(%q) ok = %v, want %v", testCase.value, ok, testCase.ok)
			}
			if got != testCase.want {
				t.Fatalf("parseMapCoordinate(%q) = %#v, want %#v", testCase.value, got, testCase.want)
			}
		})
	}
}
