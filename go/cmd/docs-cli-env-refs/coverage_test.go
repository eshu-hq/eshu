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

// TestReferenceScopeRendersEveryDiagnosticBranch pins the command-scope suffix
// #6108 added to the unknown-reference diagnostic. Only the subcommand branch
// was asserted anywhere, so a wrong environment or root-level suffix could ship
// through both suites unnoticed. Each branch is now checked as an operator
// reads it.
func TestReferenceScopeRendersEveryDiagnosticBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  reference
		want string
	}{
		{
			name: "environment reference carries no command scope",
			ref:  reference{Kind: referenceKindEnv, Document: "guide.md", Value: "ESHU_NOT_REGISTERED"},
			want: "",
		},
		{
			name: "root-level flag names the bare binary",
			ref:  reference{Kind: referenceKindFlag, Document: "guide.md", Value: "--not-a-real-root-flag"},
			want: " on command `eshu`",
		},
		{
			name: "subcommand flag names its full command path",
			ref:  reference{Kind: referenceKindFlag, Document: "guide.md", Command: "graph/status", Value: "--unknown-after-and"},
			want: " on command `eshu graph status`",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := referenceScope(test.ref); got != test.want {
				t.Fatalf("referenceScope(%#v) = %q, want %q", test.ref, got, test.want)
			}
		})
	}
}

// TestScanMarkdownAttributesPrefixedEshuCommands is the #6230 regression. A
// segment that reaches `eshu` only after a `NAME=value` assignment or a `sudo`
// used to fall out of BOTH populations -- unattributed because the first field
// was not `eshu`, and unskipped because the line carries no list operator --
// so its flags were unchecked and invisible in the summary at the same time.
//
// The assertions pin the resolved command, not just the flag: the prefix is
// stripped to FIND the eshu invocation, and the scope a diagnostic names must
// still be that invocation's own subcommand path.
func TestScanMarkdownAttributesPrefixedEshuCommands(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"ESHU_PPROF_ADDR=127.0.0.1:0 eshu docs verify --after-env\n" +
		"sudo eshu docs verify --after-sudo\n" +
		"sudo ESHU_PPROF_ADDR=127.0.0.1:0 eshu docs verify --after-both\n" +
		"eshu docs verify --json | sudo eshu graph status --after-sudo-in-pipe\n" +
		"sudo docker compose logs eshu --not-an-eshu-flag\n" +
		"```\n"
	refs, counts := scanMarkdown("guide.md", content)
	if counts.AttributedSegments != 5 {
		t.Fatalf("AttributedSegments = %d, want 5 (three prefixed lines plus both pipeline segments)", counts.AttributedSegments)
	}
	if counts.SkippedLines != 0 {
		t.Fatalf("SkippedLines = %d, want 0 (every line is inside the supported grammar)", counts.SkippedLines)
	}

	scopes := map[string]string{}
	for _, ref := range refs {
		if ref.Kind == referenceKindFlag {
			scopes[ref.Value] = ref.Command
		}
	}
	for flag, want := range map[string]string{
		"--after-env":          "docs/verify",
		"--after-sudo":         "docs/verify",
		"--after-both":         "docs/verify",
		"--after-sudo-in-pipe": "graph/status",
	} {
		got, ok := scopes[flag]
		if !ok {
			t.Fatalf("scanMarkdown() did not attribute %s at all; flags = %#v", flag, scopes)
		}
		if got != want {
			t.Fatalf("scanMarkdown() attributed %s to command %q, want %q", flag, got, want)
		}
	}
	// `sudo` is stripped, not treated as a synonym for `eshu`: a real non-Eshu
	// command behind a prefix stays out of scope even with `eshu` on the line.
	if _, ok := scopes["--not-an-eshu-flag"]; ok {
		t.Fatalf("scanMarkdown() attributed a flag on a non-Eshu command; flags = %#v", scopes)
	}
}

// TestMentionsEshuCommandSeesPrefixedInvocations keeps the skipped-line
// counter and the attribution path agreeing on what an eshu command line is
// (#6230). A prefixed line the segment grammar still cannot parse must land in
// the skipped population rather than vanishing from both.
func TestMentionsEshuCommandSeesPrefixedInvocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "env prefix", line: "ESHU_PPROF_ADDR=127.0.0.1:0 eshu graph start 2>&1 &", want: true},
		{name: "sudo prefix", line: "sudo eshu graph start 2>&1 &", want: true},
		{name: "console prompt then sudo", line: "$ sudo eshu graph start 2>&1 &", want: true},
		{name: "sudo with an option word", line: "sudo -u builder eshu graph start 2>&1 &", want: false},
		{name: "eshu only as an argument", line: "sudo docker compose logs eshu 2>&1 &", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := mentionsEshuCommand(test.line); got != test.want {
				t.Fatalf("mentionsEshuCommand(%q) = %v, want %v", test.line, got, test.want)
			}
		})
	}
}
