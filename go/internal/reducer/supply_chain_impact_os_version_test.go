// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestCompareDPKGVersionOrdersEpochRevisionAndTildeBackports(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "epoch wins before upstream", a: "2:1.0-1", b: "1:9.9-1", want: 1},
		{name: "debian revision orders backport updates", a: "3.0.11-1~deb12u2", b: "3.0.11-1~deb12u3", want: -1},
		{name: "tilde sorts before final revision", a: "1.0-1~deb12u1", b: "1.0-1", want: -1},
		{name: "implicit revision equals zero revision", a: "1.0", b: "1.0-0", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := compareDPKGVersion(tc.a, tc.b)
			if !ok {
				t.Fatalf("compareDPKGVersion(%q, %q) not ok", tc.a, tc.b)
			}
			assertCompareSign(t, got, tc.want)
		})
	}
}

func TestCompareAPKVersionOrdersSuffixesAndReleaseRevisions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "release revision orders fixed apk branch", a: "3.1.4-r5", b: "3.1.4-r6", want: -1},
		{name: "release candidate sorts before final", a: "3.1.4_rc1-r0", b: "3.1.4-r0", want: -1},
		{name: "patch suffix sorts after final", a: "3.1.4_p1-r0", b: "3.1.4-r0", want: 1},
		{name: "numeric main segments order naturally", a: "3.1.10-r0", b: "3.1.4-r9", want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := compareAPKVersion(tc.a, tc.b)
			if !ok {
				t.Fatalf("compareAPKVersion(%q, %q) not ok", tc.a, tc.b)
			}
			assertCompareSign(t, got, tc.want)
		})
	}
}

func assertCompareSign(t *testing.T, got int, want int) {
	t.Helper()
	switch {
	case got < 0 && want < 0:
		return
	case got > 0 && want > 0:
		return
	case got == 0 && want == 0:
		return
	default:
		t.Fatalf("comparison sign = %d, want %d", got, want)
	}
}
