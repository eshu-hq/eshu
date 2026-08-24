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
	got, _ := scanMarkdown("guide.md", content)
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
	got, _ := scanMarkdown("guide.md", content)
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
	got, _ := scanMarkdown("reference/local-testing/first-five-minutes-benchmark.md", content)
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
	got, _ := scanMarkdown("guide.md", content)
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
	got, _ := scanMarkdown("guide.md", content)
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
			got, _ := scanMarkdown("guide.md", "```bash\n"+test.line+"\n```\n")
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
			got, counts := scanMarkdown("guide.md", "```bash\n"+line+"\n```\n")
			if len(got) != 0 {
				t.Fatalf("scanMarkdown(%q) = %#v, want the unsupported form skipped", line, got)
			}
			// Positive half: the line must be COUNTED as skipped, not merely
			// produce no references. A scanner that stopped reading the fence
			// entirely also produces no references.
			if counts.SkippedLines != 1 {
				t.Fatalf("scanMarkdown(%q) skipped = %d, want the skip reported once", line, counts.SkippedLines)
			}
		})
	}
}

// TestScanMarkdownReportsSkippedEshuLines makes the deliberate
// under-approximation observable. A gate that silently inspects nothing and
// exits 0 is indistinguishable from a clean run, so the scanner counts the
// Eshu command lines it declined to parse and the verifier reports the number.
func TestScanMarkdownReportsSkippedEshuLines(t *testing.T) {
	t.Parallel()

	content := "```bash\n" +
		"eshu docs verify --json || eshu docs verify --after-or\n" +
		"(eshu docs verify --json ; eshu docs verify --in-subshell)\n" +
		"eshu docs verify --json | eshu graph status --checked\n" +
		"cd go && go build ./cmd/eshu\n" +
		"docker compose logs eshu | rg BOOTSTRAP\n" +
		"```\n"
	got, counts := scanMarkdown("guide.md", content)
	want := []reference{
		{Kind: referenceKindFlag, Document: "guide.md", Command: "docs/verify", Value: "--json"},
		{Kind: referenceKindFlag, Document: "guide.md", Command: "graph/status", Value: "--checked"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scanMarkdown() refs = %#v, want %#v", got, want)
	}
	// Only the `||` list and the subshell are unsupported AND mention eshu. The
	// supported `&&` and `|` lines are parsed, not skipped, so they never count.
	if counts.SkippedLines != 2 {
		t.Fatalf("scanMarkdown() skipped = %d, want 2", counts.SkippedLines)
	}
}

// TestScanMarkdownHonoursBackslashEscapesInsideDoubleQuotes is the reviewer's
// case on PR #6239. A `\"` inside a double-quoted word does not close the
// quote, so the operator behind it is not a segment boundary. A scanner that
// closes the quote there splits the line, the tail segment no longer starts
// with `eshu`, and every flag on it silently escapes validation -- a false
// negative in exactly the class #6108 exists to close.
func TestScanMarkdownHonoursBackslashEscapesInsideDoubleQuotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want []reference
	}{
		{
			name: "escaped quote hides a pipe",
			line: `eshu docs verify "a\"|b" --stale-behind-escaped-quote`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: `docs/verify/a"|b`, Value: "--stale-behind-escaped-quote"}},
		},
		{
			name: "escaped quote hides an and list",
			line: `eshu docs verify "a\"&&b" --stale-behind-escaped-and`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: `docs/verify/a"&&b`, Value: "--stale-behind-escaped-and"}},
		},
		{
			name: "escaped quote hides a semicolon",
			line: `eshu docs verify "a\";b" --stale-behind-escaped-semicolon`,
			want: []reference{{Kind: referenceKindFlag, Document: "guide.md", Command: `docs/verify/a";b`, Value: "--stale-behind-escaped-semicolon"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, _ := scanMarkdown("guide.md", "```bash\n"+test.line+"\n```\n")
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("scanMarkdown(%q) = %#v, want %#v", test.line, got, test.want)
			}
		})
	}
}

// TestScanMarkdownStillSplitsWhereTheQuoteReallyCloses is the other half of the
// escape rule, and the guard against over-correcting it. An escaped backslash
// consumes only itself, so the quote that follows it really does close and the
// operator after it really is a boundary; a single-quoted word processes no
// escapes at all, exactly as POSIX sh and splitShellFields treat it.
func TestScanMarkdownStillSplitsWhereTheQuoteReallyCloses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want []reference
	}{
		{
			name: "escaped backslash lets the closing quote close",
			line: `eshu docs verify "a\\" --before-real-pipe-invalid | eshu graph status --after-real-pipe`,
			want: []reference{
				{Kind: referenceKindFlag, Document: "guide.md", Command: `docs/verify/a\`, Value: "--before-real-pipe-invalid"},
				{Kind: referenceKindFlag, Document: "guide.md", Command: "graph/status", Value: "--after-real-pipe"},
			},
		},
		{
			name: "single quotes process no escape",
			line: `eshu docs verify 'a\' --before-single-quote-pipe-invalid | eshu graph status --after-single-quote-pipe`,
			want: []reference{
				{Kind: referenceKindFlag, Document: "guide.md", Command: `docs/verify/a\`, Value: "--before-single-quote-pipe-invalid"},
				{Kind: referenceKindFlag, Document: "guide.md", Command: "graph/status", Value: "--after-single-quote-pipe"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, _ := scanMarkdown("guide.md", "```bash\n"+test.line+"\n```\n")
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("scanMarkdown(%q) = %#v, want %#v", test.line, got, test.want)
			}
		})
	}
}

// TestScanMarkdownCountsOnlyEshuInvocationsAsSkippedLines pins what the skipped
// count means. Since #6108 the count is asserted exactly in both directions, so
// a line that merely carries the word `eshu` as an argument -- a container name,
// an rg pattern, a quoted literal -- must not inflate it: over-reporting turns
// an unrelated future docs edit into a gate failure. An `eshu` invocation behind
// leading environment assignments still counts, because that one really is a
// command line the scanner declined to parse.
func TestScanMarkdownCountsOnlyEshuInvocationsAsSkippedLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want int
	}{
		{
			name: "eshu as a container argument",
			line: `docker compose logs eshu 2>&1`,
			want: 0,
		},
		{
			name: "eshu as a quoted literal and a search pattern",
			line: `echo "eshu docs verify" | rg eshu 2>&1`,
			want: 0,
		},
		{
			name: "backgrounded eshu behind environment assignments",
			line: `ESHU_PPROF_ADDR=127.0.0.1:0 eshu graph start --logs terminal > /tmp/run.log 2>&1 &`,
			want: 1,
		},
		{
			name: "eshu invocation on an unsupported or list",
			line: `eshu docs verify --json || eshu docs verify --after-or`,
			want: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, counts := scanMarkdown("guide.md", "```bash\n"+test.line+"\n```\n")
			if counts.SkippedLines != test.want {
				t.Fatalf("scanMarkdown(%q) skipped = %d, want %d", test.line, counts.SkippedLines, test.want)
			}
		})
	}
}

// TestScanMarkdownKeepsGroupingOutOfScopeWithoutAListOperator covers the shape
// the unsupported-form suite missed: grouping and substitution on a line that
// carries no list operator at all. Command substitution is documented as out of
// scope, so `$(echo --fake-flag )` must not resolve `--fake-flag` against the
// command `eshu docs verify/$(echo` and fail the gate on a form the grammar
// never claimed.
func TestScanMarkdownKeepsGroupingOutOfScopeWithoutAListOperator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		line        string
		wantSkipped int
	}{
		{
			name:        "dollar substitution with no list operator",
			line:        `eshu docs verify $(echo --fake-flag )`,
			wantSkipped: 1,
		},
		{
			name:        "backtick substitution with no list operator",
			line:        "eshu docs verify `echo x` --backtick-invalid",
			wantSkipped: 1,
		},
		{
			name:        "subshell group with no list operator",
			line:        `(eshu docs verify --in-subshell)`,
			wantSkipped: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, counts := scanMarkdown("guide.md", "```bash\n"+test.line+"\n```\n")
			if len(got) != 0 {
				t.Fatalf("scanMarkdown(%q) = %#v, want the unsupported form skipped", test.line, got)
			}
			if counts.SkippedLines != test.wantSkipped {
				t.Fatalf("scanMarkdown(%q) skipped = %d, want %d", test.line, counts.SkippedLines, test.wantSkipped)
			}
		})
	}
}
