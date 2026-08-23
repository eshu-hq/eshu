// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"
)

// TestScanMarkdownAttributesPipelineSegmentsToTheirOwnCommands pins the #6108
// contract: each literal Eshu segment of a simple pipeline or list owns its own
// flags. The former behaviour skipped the whole logical line.
func TestScanMarkdownAttributesPipelineSegmentsToTheirOwnCommands(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"eshu docs verify --json | eshu graph status --unknown-after-pipe\n" +
		"eshu docs verify --json && eshu graph status --unknown-after-and\n" +
		"eshu docs verify --json ; eshu graph status --unknown-after-semicolon\n" +
		"```\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--json"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "graph/status", Value: "--unknown-after-and"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "graph/status", Value: "--unknown-after-pipe"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "graph/status", Value: "--unknown-after-semicolon"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

// TestScanMarkdownHostileFlagCollisionAcrossPipelineSegments is the most
// important case in #6108. `--report-out` is a real `eshu first-run` flag and is
// NOT a flag of `eshu first-run-benchmark`. A splitter that folds the second
// segment's flags into the first command resolves it against `first-run` and
// silently passes, hiding a stale documented flag.
func TestScanMarkdownHostileFlagCollisionAcrossPipelineSegments(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"eshu first-run --json | eshu first-run-benchmark --report-out /tmp/first-run.md\n" +
		"```\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "first-run", Value: "--json"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "first-run-benchmark", Value: "--report-out"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

// TestScanMarkdownChecksDocumentedFirstRunBenchmarkPipeline covers the example
// named in #6108. The flags are real: `--json` belongs to `eshu first-run` and
// `--path` belongs to `eshu first-run-benchmark` only. Folding them together
// would report a false failure on whichever command did not own the flag.
func TestScanMarkdownChecksDocumentedFirstRunBenchmarkPipeline(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"eshu first-run --json | eshu first-run-benchmark --path local_binary\n" +
		"```\n"
	got := scanMarkdown("reference/local-testing/first-five-minutes-benchmark.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "reference/local-testing/first-five-minutes-benchmark.md", Command: "first-run", Value: "--json"},
		{Kind: referenceKindFlag, Document: "reference/local-testing/first-five-minutes-benchmark.md", Command: "first-run-benchmark", Value: "--path"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

// TestScanMarkdownScansEshuSegmentAfterNonEshuPipelineStage covers the only
// literal pipeline in docs/public today:
// docs/public/reference/service-intelligence-report.md.
func TestScanMarkdownScansEshuSegmentAfterNonEshuPipelineStage(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"cat service-story.json | eshu service-report --not-a-real-flag   # or pipe on stdin\n" +
		"```\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "service-report", Value: "--not-a-real-flag"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

// TestScanMarkdownSegmentsAPipelineWrappedOverContinuationLines covers the shape
// docs actually use for a long pipeline: a backslash continuation splits it over
// physical lines, and the scanner segments the joined logical line.
func TestScanMarkdownSegmentsAPipelineWrappedOverContinuationLines(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"$ eshu first-run --json \\\n" +
		"  | eshu first-run-benchmark --report-out /tmp/first-run.md\n" +
		"```\n"
	got := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "first-run", Value: "--json"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "first-run-benchmark", Value: "--report-out"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() = %#v, want %#v", got, want)
	}
}

// TestScanMarkdownKeepsQuotedEscapedAndCommentedOperatorsOutOfSegmentBoundaries
// proves the boundary scanner never splits on an operator that the shell would
// not treat as one.
func TestScanMarkdownKeepsQuotedEscapedAndCommentedOperatorsOutOfSegmentBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want []reference
	}{
		{
			name: "double quoted pipe",
			line: `eshu docs verify "a|b" --quoted-pipe-invalid`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify/a|b", Value: "--quoted-pipe-invalid"}},
		},
		{
			name: "single quoted semicolon",
			line: `eshu docs verify 'a;b' --quoted-semicolon-invalid`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify/a;b", Value: "--quoted-semicolon-invalid"}},
		},
		{
			name: "escaped ampersand",
			line: `eshu docs verify a\&\&b --escaped-ampersand-invalid`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify/a&&b", Value: "--escaped-ampersand-invalid"}},
		},
		{
			name: "escaped pipe",
			line: `eshu docs verify --escaped-pipe-invalid \| more`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--escaped-pipe-invalid"}},
		},
		{
			name: "operators inside trailing comment",
			line: `eshu docs verify --commented-operator-invalid # note | other && third ; fourth`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--commented-operator-invalid"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := scanMarkdown("guide.md", "```bash\n"+test.line+"\n```\n")
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("scanMarkdown(%q) = %#v, want %#v", test.line, got, test.want)
			}
		})
	}
}

// TestScanMarkdownFallsBackToSkippingUnsupportedShellForms pins the deliberate
// under-approximation: anything outside the documented simple-list grammar keeps
// the pre-#6108 skip rather than being guessed at.
func TestScanMarkdownFallsBackToSkippingUnsupportedShellForms(t *testing.T) {
	t.Parallel()

	lines := map[string]string{
		"or list":               `eshu docs verify --json || eshu docs verify --after-or`,
		"background ampersand":  `eshu docs verify --json & eshu docs verify --after-background`,
		"case terminator":       `eshu docs verify --json ;; eshu docs verify --after-double-semicolon`,
		"pipe ampersand":        `eshu docs verify --json |& eshu docs verify --after-pipe-ampersand`,
		"stderr redirect":       `eshu docs verify --json 2>&1 ; eshu docs verify --after-redirect`,
		"backtick substitution": "eshu docs verify --json `echo a | eshu docs verify --inside-backticks`",
		"dollar substitution":   `eshu docs verify --json $(echo a | eshu docs verify --inside-substitution)`,
		"subshell group":        `(eshu docs verify --json ; eshu docs verify --inside-subshell)`,
		"leading separator":     `| eshu docs verify --after-leading-separator`,
		"trailing separator":    `eshu docs verify --before-trailing-separator |`,
	}
	for name, line := range lines {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := scanMarkdown("guide.md", "```bash\n"+line+"\n```\n"); len(got) != 0 {
				t.Fatalf("scanMarkdown(%q) = %#v, want the unsupported form skipped", line, got)
			}
		})
	}
}
