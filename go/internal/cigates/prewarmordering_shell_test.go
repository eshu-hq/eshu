// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"
)

// This file is PR #6743's Codex P1 fix: the pre-warm was recognized by raw
// text match, so a commented-out call, an echoed path, or a mention inside
// printf/cat still counted as "warms that module" -- silently reopening the
// cache race the guard exists to catch. Cases (a)-(i) below are the
// reviewer's own list. Shares buildPrewarmRepo/prewarmOrderingErrs with
// prewarmordering_test.go -- same package, same directory.

// TestCheckSetupGoPrewarmOrdering_CommentedOut_Violation is case (a): a
// commented-out pre-warm line followed by a real Go command. Text-match
// credited the comment as a real invocation; command-position matching must
// not.
func TestCheckSetupGoPrewarmOrdering_CommentedOut_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm (commented out)
        run: |
          # scripts/ci/go-mod-download-retry.sh
          go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("commented-out fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_EchoedPath_Violation is case (b): the
// script's path is only an argument to `echo`, never executed.
func TestCheckSetupGoPrewarmOrdering_EchoedPath_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Announce (not run) the pre-warm
        run: echo scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("echoed-path fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_PrintfPath_Violation is case (c): same
// shape as (b), via printf instead of echo.
func TestCheckSetupGoPrewarmOrdering_PrintfPath_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Announce (not run) the pre-warm
        run: printf '%s' scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("printf-path fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_TrailingComment_Clean is case (d): a real,
// executed call with a trailing shell comment must still be recognized.
func TestCheckSetupGoPrewarmOrdering_TrailingComment_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm
        run: scripts/ci/go-mod-download-retry.sh # warm
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("trailing-comment fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_LeadingAssignment_Clean is case (e): a
// leading VAR=value assignment before the real call must not defeat
// recognition, and the module argument after it must still be extracted.
func TestCheckSetupGoPrewarmOrdering_LeadingAssignment_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: sdk/go/collector/go.mod
          cache-dependency-path: sdk/go/collector/go.mod
      - name: Pre-warm
        run: GOFLAGS=-mod=mod scripts/ci/go-mod-download-retry.sh sdk/go/collector
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("leading-assignment fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_BashPrefixed_Clean is case (f): `bash
// scripts/ci/go-mod-download-retry.sh` (as several real jobs in this repo's
// sibling scripts use for OTHER scripts) must still be recognized as
// executing the pre-warm.
func TestCheckSetupGoPrewarmOrdering_BashPrefixed_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm
        run: bash scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("bash-prefixed fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_RelativeDotDotPath_Violation is case (g):
// `cd go && ../scripts/ci/go-mod-download-retry.sh`. DECISION: a "../"
// prefix is NOT recognized -- resolving it needs the step's actual working
// directory (the `working-directory:` field, or any preceding `cd` this
// check does not track), which is genuinely ambiguous to resolve
// statically. No real job in this repo uses this form (confirmed: every
// real invocation is the bare `scripts/ci/go-mod-download-retry.sh`); this
// is a documented limit, not a guess. See prewarmScriptPaths' doc comment.
func TestCheckSetupGoPrewarmOrdering_RelativeDotDotPath_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm (relative from a cd'd directory)
        run: cd go && ../scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("relative-dotdot-path fixture: got %d violations, want 1 (documented limit -- ../ not recognized): %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_HashInsideQuote_Clean is case (h): a `#`
// inside a quoted string on the SAME line as a real call must not be
// mistaken for a comment start that truncates the command.
func TestCheckSetupGoPrewarmOrdering_HashInsideQuote_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm
        run: scripts/ci/go-mod-download-retry.sh && echo "issue #6615 warm"
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("hash-inside-quote fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_IfGuarded_Violation is case (i): the
// pre-warm inside `if scripts/ci/go-mod-download-retry.sh; then`. DECISION:
// treated as not warming. The command-splitter's keyword list is exactly
// {then, do, else} per the review's own spec -- "if" is deliberately NOT a
// split keyword, so "if scripts/ci/go-mod-download-retry.sh" stays ONE
// command whose first word is "if", not the script path, and is never
// recognized as a pre-warm invocation at all. That also matches the
// reviewer's own reasoning (the exit status is consumed by the `if`), so no
// separate case-(i)-specific logic was needed.
func TestCheckSetupGoPrewarmOrdering_IfGuarded_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm, but only if it happens to succeed
        run: |
          if scripts/ci/go-mod-download-retry.sh; then
            echo warmed
          fi
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("if-guarded fixture: got %d violations, want 1: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_PipelineSecondStage_Clean proves the
// operator-splitting itself: a pre-warm as the second stage of a `&&`
// pipeline (already covered by case e/h implicitly, but this isolates
// operator-splitting alone, with no assignment or trailing text) is
// recognized.
func TestCheckSetupGoPrewarmOrdering_PipelineSecondStage_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm after an unrelated echo
        run: echo starting && scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("pipeline-second-stage fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_DotSlashPrefixed_Clean proves the explicit
// "./"-prefixed form (unlike the "../" form in case g) IS recognized: it
// needs no working-directory resolution, just a literal string match.
func TestCheckSetupGoPrewarmOrdering_DotSlashPrefixed_Clean(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Pre-warm
        run: ./scripts/ci/go-mod-download-retry.sh
      - name: Build
        run: go build ./...
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 0 {
		t.Fatalf("dot-slash-prefixed fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrdering_StillOrdered_Violation proves the ordering
// check still works against the EXECUTING command's position, not the first
// textual match: a comment mentioning the script BEFORE a real touch, with
// the real (executing) pre-warm call coming only AFTER the touch, is still
// a violation -- the comment must not count as "seen before the touch"
// either.
func TestCheckSetupGoPrewarmOrdering_StillOrdered_Violation(t *testing.T) {
	t.Parallel()

	root := buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Mixed
        run: |
          # scripts/ci/go-mod-download-retry.sh
          go build ./...
          scripts/ci/go-mod-download-retry.sh
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("still-ordered fixture: got %d violations, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "go build") && !strings.Contains(got[0], "Go command") {
		t.Fatalf("still-ordered fixture: message %q does not name the touch", got[0])
	}
}
