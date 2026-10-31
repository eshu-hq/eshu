// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestBaseScoreMatchesFIRSTExamples(t *testing.T) {
	t.Parallel()

	for _, tc := range append(append([]referenceVector{}, firstV31Examples...), firstV40Examples...) {
		t.Run(tc.vector, func(t *testing.T) {
			t.Parallel()
			got, err := BaseScore(tc.vector)
			if err != nil {
				t.Fatalf("BaseScore(%q) error = %v, want nil", tc.vector, err)
			}
			if got.Score != tc.score {
				t.Fatalf("BaseScore(%q) = %v, want FIRST's %v", tc.vector, got.Score, tc.score)
			}
		})
	}
}

func TestBaseScoreReportsVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		vector string
		want   string
	}{
		{"CVSS:3.0/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", "3.0"},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", "3.1"},
		{"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N", "4.0"},
	}
	for _, tc := range cases {
		got, err := BaseScore(tc.vector)
		if err != nil {
			t.Fatalf("BaseScore(%q) error = %v", tc.vector, err)
		}
		if got.Version != tc.want {
			t.Errorf("BaseScore(%q).Version = %q, want %q", tc.vector, got.Version, tc.want)
		}
	}
}

// TestBaseScoreIgnoresThreatAndEnvironmentalGroups pins that BaseScore returns
// the base score even when the vector also carries threat or environmental
// metrics. Each expected value is the score the FIRST examples page
// (https://www.first.org/cvss/examples) lists beside the base-only form of the
// same vector (the v3.1 temporal case is listed on that page with 6.5).
func TestBaseScoreIgnoresThreatAndEnvironmentalGroups(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		vector string
		want   float64
	}{
		{
			name:   "v4 threat metric E:U",
			vector: "CVSS:4.0/AV:N/AC:L/AT:P/PR:N/UI:P/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N/E:U",
			want:   7.7,
		},
		{
			name:   "v4 environmental metrics",
			vector: "CVSS:4.0/AV:N/AC:L/AT:P/PR:N/UI:N/VC:H/VI:L/VA:L/SC:N/SI:N/SA:N/CR:H/IR:L/AR:L/MAV:N/MAC:H/MVC:H/MVI:L/MVA:L",
			want:   8.3,
		},
		{
			name:   "v4 supplemental metrics",
			vector: "CVSS:4.0/AV:N/AC:L/AT:P/PR:N/UI:N/VC:H/VI:L/VA:L/SC:N/SI:N/SA:N/S:P/AU:Y/R:I/V:D/RE:H/U:Red",
			want:   8.3,
		},
		{
			name:   "v3.1 temporal metrics",
			vector: "CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:U/C:N/I:N/A:H/E:U/RL:O/RC:C",
			want:   6.5,
		},
	}
	for _, tc := range cases {
		got, err := BaseScore(tc.vector)
		if err != nil {
			t.Fatalf("%s: BaseScore error = %v", tc.name, err)
		}
		if got.Score != tc.want {
			t.Errorf("%s: BaseScore = %v, want %v", tc.name, got.Score, tc.want)
		}
	}
}

func TestBaseScoreNoImpactIsZero(t *testing.T) {
	t.Parallel()

	for _, vector := range []string{
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N",
		"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:N/SC:N/SI:N/SA:N",
	} {
		got, err := BaseScore(vector)
		if err != nil {
			t.Fatalf("BaseScore(%q) error = %v", vector, err)
		}
		if got.Score != 0 {
			t.Errorf("BaseScore(%q) = %v, want 0", vector, got.Score)
		}
	}
}

func TestBaseScoreRejectsMalformedVectors(t *testing.T) {
	t.Parallel()

	const v31 = "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"
	const v40 = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N"
	malformed := map[string]string{
		"empty":                    "",
		"blank":                    "   ",
		"free text":                "not a vector",
		"prefix only":              "CVSS:3.1",
		"prefix and slash":         "CVSS:3.1/",
		"missing base metric v3":   "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H",
		"missing base metric v4":   "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N",
		"duplicate metric v3":      v31 + "/AV:L",
		"duplicate metric v4":      v40 + "/AV:L",
		"invalid value v3":         "CVSS:3.1/AV:Z/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"invalid value v4":         "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:Q/VI:H/VA:H/SC:N/SI:N/SA:N",
		"unknown metric v3":        v31 + "/ZZ:N",
		"unknown metric v4":        v40 + "/ZZ:N",
		"v4 metric in v3 vector":   v31 + "/AT:N",
		"empty segment":            "CVSS:3.1/AV:N//AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"trailing slash":           v31 + "/",
		"metric without value":     "CVSS:3.1/AV/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"metric with empty value":  "CVSS:3.1/AV:/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"extra colon":              "CVSS:3.1/AV:N:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"lowercase value":          "CVSS:3.1/AV:n/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"NUL byte":                 "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H\x00",
		"invalid threat value":     v40 + "/E:Z",
		"invalid env value v3":     v31 + "/CR:Q",
		"v3 scope value in v4":     "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N/MSI:Q",
		"trailing bare segment":    v31 + "/X",
		"unicode lookalike prefix": "СVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}
	for name, vector := range malformed {
		got, err := BaseScore(vector)
		if err == nil {
			t.Errorf("%s: BaseScore(%q) = %+v, want an error", name, vector, got)
			continue
		}
		if !errors.Is(err, ErrMalformedVector) && !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("%s: error %v is neither ErrMalformedVector nor ErrUnsupportedVersion", name, err)
		}
		if got.Score != 0 {
			t.Errorf("%s: score = %v on error, want 0", name, got.Score)
		}
	}
}

func TestBaseScoreRejectsUnsupportedVersions(t *testing.T) {
	t.Parallel()

	for _, vector := range []string{
		"AV:N/AC:L/Au:N/C:C/I:C/A:C",
		"CVSS:2.0/AV:N/AC:L/Au:N/C:C/I:C/A:C",
		"CVSS:5.0/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"CVSS:3.2/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	} {
		_, err := BaseScore(vector)
		if !errors.Is(err, ErrUnsupportedVersion) && !errors.Is(err, ErrMalformedVector) {
			t.Errorf("BaseScore(%q) error = %v, want a typed error", vector, err)
		}
	}
}

func TestBaseScoreTrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	got, err := BaseScore("  CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H\n")
	if err != nil {
		t.Fatalf("BaseScore error = %v", err)
	}
	if got.Score != 9.8 {
		t.Fatalf("BaseScore = %v, want 9.8", got.Score)
	}
}

func TestBaseScoreIsFiniteAndInRange(t *testing.T) {
	t.Parallel()

	for _, tc := range append(append([]referenceVector{}, firstV31Examples...), firstV40Examples...) {
		got, err := BaseScore(tc.vector)
		if err != nil {
			t.Fatalf("BaseScore(%q) error = %v", tc.vector, err)
		}
		if math.IsNaN(got.Score) || math.IsInf(got.Score, 0) || got.Score < 0 || got.Score > 10 {
			t.Fatalf("BaseScore(%q) = %v, want a finite score in [0, 10]", tc.vector, got.Score)
		}
	}
}

func FuzzBaseScoreNeverPanics(f *testing.F) {
	f.Add("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H")
	f.Add("CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N")
	f.Add("CVSS:3.1/")
	f.Add("")
	f.Fuzz(func(t *testing.T, vector string) {
		got, err := BaseScore(vector)
		if err != nil {
			if got.Score != 0 {
				t.Fatalf("score %v on error %v", got.Score, err)
			}
			return
		}
		if math.IsNaN(got.Score) || got.Score < 0 || got.Score > 10 {
			t.Fatalf("BaseScore(%q) = %v, want [0, 10]", vector, got.Score)
		}
	})
}

// TestBaseScoreV30 pins v3.0 vectors to the scores the FIRST CVSS v3.0
// calculator (https://www.first.org/cvss/calculator/cvsscalc30.js, retrieved
// 2026-10-30) reports for them.
func TestBaseScoreV30(t *testing.T) {
	t.Parallel()

	cases := []referenceVector{
		{"CVSS:3.0/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8},
		{"CVSS:3.0/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H", 10.0},
		{"CVSS:3.0/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N", 6.1},
		{"CVSS:3.0/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H", 7.8},
		{"CVSS:3.0/AV:P/AC:H/PR:H/UI:R/S:U/C:L/I:N/A:N", 1.6},
	}
	for _, tc := range cases {
		got, err := BaseScore(tc.vector)
		if err != nil {
			t.Fatalf("BaseScore(%q) error = %v", tc.vector, err)
		}
		if got.Score != tc.score {
			t.Errorf("BaseScore(%q) = %v, want %v", tc.vector, got.Score, tc.score)
		}
	}
}

// TestBaseScoreRefusesOversizedVectorsBeforeSplitting pins the length cap: a
// vector longer than any real one is refused as too long without being split
// into segments first, so a hostile multi-megabyte value costs one length check.
func TestBaseScoreRefusesOversizedVectorsBeforeSplitting(t *testing.T) {
	t.Parallel()

	oversized := "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" + strings.Repeat("/E:X", 1000)
	_, err := BaseScore(oversized)
	if !errors.Is(err, ErrMalformedVector) || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("BaseScore(oversized) error = %v, want ErrMalformedVector naming the length", err)
	}
	// The longest real vector (v4.0 with every optional metric) stays well under the cap.
	const longest = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H/E:A/CR:H/IR:H/AR:H" +
		"/MAV:N/MAC:L/MAT:N/MPR:N/MUI:N/MVC:H/MVI:H/MVA:H/MSC:H/MSI:S/MSA:S/S:P/AU:Y/R:I/V:C/RE:H/U:Amber"
	if _, err := BaseScore(longest); err != nil {
		t.Fatalf("BaseScore(longest real vector, %d bytes) error = %v", len(longest), err)
	}
}
