// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "testing"

func TestInFixtureDirMatchesWholeSegmentsInAnyASCIICase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path string
		want bool
	}{
		{"package.json", false},
		{"packages/core/package.json", false},
		{"tests/package.json", true},
		{"Tests/Fixtures/app/package.json", true},
		{"packages/core/tests/fixtures/app/package.json", true},
		{"FIXTURES/package.json", true},
		{"testdata/pkg/package.json", true},
		{"TestData/pkg/package.json", true},
		{"docs/examples/demo/package.json", true},
		{"Examples/package.json", true},
		{"test/assets/app/package.json", true},
		{"packages/core/Test/package.json", true},
		// A substring of a segment is not a segment.
		{"packages/testing/package.json", false},
		{"packages/attestation/package.json", false},
		{"packages/latests/package.json", false},
		{"packages/contest/package.json", false},
		{"packages/fixtures-lib/package.json", false},
		{"packages/my-examples/package.json", false},
		{"packages/latest/package.json", false},
		{"packages/tester/package.json", false},
		// ASCII-exact in both dialects: Go's (?i) folds the long s (U+017F) to
		// s while PostgreSQL's ~* does not, so the pattern spells ASCII classes.
		{"a/teſts/package.json", false},
		{"a/TEſTS/package.json", false},
		// The file name is never a directory segment.
		{"packages/tests", false},
		{"", false},
	} {
		if got := InFixtureDir(tc.path); got != tc.want {
			t.Errorf("InFixtureDir(%q) = %t, want %t", tc.path, got, tc.want)
		}
	}
}
