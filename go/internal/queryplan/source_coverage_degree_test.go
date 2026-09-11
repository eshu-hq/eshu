// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"strings"
	"testing"
)

func TestValidateSourceCoverageRejectsIncompleteDegreeBoundedEvidence(t *testing.T) {
	tests := []struct {
		name        string
		disposition NonHotDisposition
		want        string
	}{
		{
			name: "degree bounded missing key bound",
			disposition: NonHotDisposition{
				Class:        NonHotClassDegreeBounded,
				SourceDigest: strings.Repeat("a", 64),
				MaxDegree:    nonHotCorpusMaxCALLSDegree,
			},
			want: "degree_bounded requires key_bound",
		},
		{
			name: "degree bounded batch key bound",
			disposition: NonHotDisposition{
				Class:        NonHotClassDegreeBounded,
				SourceDigest: strings.Repeat("a", 64),
				KeyBound:     NonHotKeyBoundBatch,
				MaxDegree:    nonHotCorpusMaxCALLSDegree,
			},
			want: "degree_bounded requires key_bound",
		},
		{
			name: "degree bounded missing max degree",
			disposition: NonHotDisposition{
				Class:        NonHotClassDegreeBounded,
				SourceDigest: strings.Repeat("a", 64),
				KeyBound:     NonHotKeyBoundSingle,
			},
			want: "degree_bounded requires max_degree",
		},
		{
			name: "degree bounded below corpus floor",
			disposition: NonHotDisposition{
				Class:        NonHotClassDegreeBounded,
				SourceDigest: strings.Repeat("a", 64),
				KeyBound:     NonHotKeyBoundSingle,
				MaxDegree:    nonHotCorpusMaxCALLSDegree - 1,
			},
			want: "degree_bounded requires max_degree",
		},
		{
			name: "depth bounded missing max depth",
			disposition: NonHotDisposition{
				Class:        NonHotClassDepthBounded,
				SourceDigest: strings.Repeat("a", 64),
				KeyBound:     NonHotKeyBoundSingle,
				MaxDegree:    nonHotCorpusMaxCALLSDegree,
			},
			want: "depth_bounded requires max_depth",
		},
		{
			name: "depth bounded max depth below ceiling",
			disposition: NonHotDisposition{
				Class:        NonHotClassDepthBounded,
				SourceDigest: strings.Repeat("a", 64),
				KeyBound:     NonHotKeyBoundSingle,
				MaxDegree:    nonHotCorpusMaxCALLSDegree,
				MaxDepth:     nonHotTransitiveMaxDepth - 1,
			},
			want: "depth_bounded requires max_depth",
		},
		{
			name: "depth bounded max depth above ceiling",
			disposition: NonHotDisposition{
				Class:        NonHotClassDepthBounded,
				SourceDigest: strings.Repeat("a", 64),
				KeyBound:     NonHotKeyBoundSingle,
				MaxDegree:    nonHotCorpusMaxCALLSDegree,
				MaxDepth:     nonHotTransitiveMaxDepth + 1,
			},
			want: "depth_bounded requires max_depth",
		},
		{
			name: "depth bounded below corpus floor",
			disposition: NonHotDisposition{
				Class:        NonHotClassDepthBounded,
				SourceDigest: strings.Repeat("a", 64),
				KeyBound:     NonHotKeyBoundSingle,
				MaxDegree:    nonHotCorpusMaxCALLSDegree - 1,
				MaxDepth:     nonHotTransitiveMaxDepth,
			},
			want: "depth_bounded requires max_degree",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := Manifest{
				Version: 1,
				SourceCoverage: []SourceCoverage{{
					File: "handler.go",
					Calls: []QueryCallsite{{
						Symbol: "handle",
						Count:  1,
						NonHot: &test.disposition,
					}},
				}},
			}
			discovered := []SourceCoverage{{
				File: "handler.go",
				Calls: []QueryCallsite{{
					Symbol:       "handle",
					Count:        1,
					SourceDigest: strings.Repeat("a", 64),
				}},
			}}
			err := ValidateSourceCoverage(manifest, discovered)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateSourceCoverage() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateSourceCoverageAcceptsDegreeBoundedDispositions(t *testing.T) {
	digest := strings.Repeat("a", 64)
	manifest := Manifest{
		Version: 1,
		SourceCoverage: []SourceCoverage{{
			File: "callers.go",
			Calls: []QueryCallsite{
				{
					Symbol: "(*Handler).oneHop",
					Count:  1,
					NonHot: &NonHotDisposition{
						Class:        NonHotClassDegreeBounded,
						SourceDigest: digest,
						KeyBound:     NonHotKeyBoundSingle,
						MaxDegree:    nonHotCorpusMaxCALLSDegree,
					},
				},
				{
					Symbol: "(*Handler).transitive",
					Count:  1,
					NonHot: &NonHotDisposition{
						Class:        NonHotClassDepthBounded,
						SourceDigest: digest,
						KeyBound:     NonHotKeyBoundSingle,
						MaxDegree:    nonHotCorpusMaxCALLSDegree,
						MaxDepth:     nonHotTransitiveMaxDepth,
					},
				},
			},
		}},
	}
	discovered := []SourceCoverage{{
		File: "callers.go",
		Calls: []QueryCallsite{
			{Symbol: "(*Handler).oneHop", Count: 1, SourceDigest: digest},
			{Symbol: "(*Handler).transitive", Count: 1, SourceDigest: digest},
		},
	}}

	if err := ValidateSourceCoverage(manifest, discovered); err != nil {
		t.Fatalf("ValidateSourceCoverage() error = %v, want degree-bounded dispositions accepted", err)
	}
}
