// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// TestScanMarkdownCountsAttributedEshuSegments pins the denominator that makes
// the skipped count readable: how many Eshu command segments the parser
// actually attributed flags to.
func TestScanMarkdownCountsAttributedEshuSegments(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"eshu docs verify --json | eshu graph status --checked\n" +
		"cat story.json | eshu service-report\n" +
		"cd go && go build ./cmd/eshu\n" +
		"eshu docs verify --json || eshu docs verify --after-or\n" +
		"```\n"
	_, counts := scanMarkdown("guide.md", content)
	if counts.AttributedSegments != 3 {
		t.Fatalf("AttributedSegments = %d, want 3 (two pipeline segments plus the one after cat)", counts.AttributedSegments)
	}
	if counts.SkippedLines != 1 {
		t.Fatalf("SkippedLines = %d, want 1 (the || list)", counts.SkippedLines)
	}
}

// TestValidateScanCoverageFailsInBothDirections proves the skipped-line
// population is an ASSERTED pin rather than a printed decoration, and that a
// collapse in attributed coverage fails even when nothing is skipped.
func TestValidateScanCoverageFailsInBothDirections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		counts scanCounts
		want   string
	}{
		{
			name:   "skip population grew",
			counts: scanCounts{AttributedSegments: 100, SkippedLines: 2},
			want:   "grew",
		},
		{
			name:   "skip population shrank without a re-pin",
			counts: scanCounts{AttributedSegments: 100, SkippedLines: 0},
			want:   "re-pin",
		},
		{
			name:   "attributed coverage collapsed",
			counts: scanCounts{AttributedSegments: 5, SkippedLines: 1},
			want:   "floor",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateScanCoverage(test.counts, 1, 10)
			if err == nil {
				t.Fatalf("validateScanCoverage(%#v) error = nil, want rejection", test.counts)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateScanCoverage(%#v) error = %q, want it to name %q", test.counts, err, test.want)
			}
		})
	}

	if err := validateScanCoverage(scanCounts{AttributedSegments: 100, SkippedLines: 1}, 1, 10); err != nil {
		t.Fatalf("validateScanCoverage() on the pinned shape error = %v, want nil", err)
	}
}
