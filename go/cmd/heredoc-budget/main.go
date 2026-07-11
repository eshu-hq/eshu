// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command heredoc-budget is documented in doc.go.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// defaultBudget is the default per-heredoc-body byte budget: the size past
// which a `<<EOF`-style heredoc risks deadlocking under Homebrew bash >= 5.1
// on macOS (see doc.go and #5074).
const defaultBudget = 512

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the CLI. It is separated from main so tests can drive it
// with fake arguments and captured output instead of a real process.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("heredoc-budget", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baselinePath := fs.String("baseline", "", "path to the heredoc-budget baseline file (required)")
	update := fs.Bool("update", false, "regenerate the baseline from the current tree instead of checking it")
	budget := fs.Int("budget", defaultBudget, "byte budget per heredoc body")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *baselinePath == "" {
		_, _ = fmt.Fprintln(stderr, "heredoc-budget: -baseline is required")
		return 2
	}

	scanRoot := filepath.Dir(*baselinePath)
	current, err := ScanTree(scanRoot, *budget)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "heredoc-budget: scan failed: %v\n", err)
		return 1
	}

	if *update {
		return runUpdate(*baselinePath, *budget, current, stdout, stderr)
	}
	return runCheck(*baselinePath, *budget, current, stdout, stderr)
}

// runUpdate regenerates the baseline file from the current scan and reports
// a short summary.
func runUpdate(baselinePath string, budget int, current map[string][]Violation, stdout, stderr io.Writer) int {
	counts := make(map[string]int, len(current))
	for path, vs := range current {
		counts[path] = len(vs)
	}
	if err := os.WriteFile(baselinePath, []byte(RenderBaseline(counts)), 0o644); err != nil { // #nosec G306 -- baseline is a checked-in text artifact, not sensitive data.
		_, _ = fmt.Fprintf(stderr, "heredoc-budget: writing baseline %s: %v\n", baselinePath, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "heredoc-budget: baseline updated: %d file(s) with a heredoc over %d bytes\n", len(counts), budget)
	return 0
}

// runCheck compares the current scan against the existing baseline file and
// reports PASS/FAIL.
func runCheck(baselinePath string, budget int, current map[string][]Violation, stdout, stderr io.Writer) int {
	baselineFile, err := os.Open(baselinePath) // #nosec G304 -- baselinePath is an operator-supplied CLI flag, not external/untrusted input.
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "heredoc-budget: opening baseline %s: %v\n", baselinePath, err)
		return 1
	}
	defer func() { _ = baselineFile.Close() }()

	baseline, err := ParseBaseline(baselineFile)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "heredoc-budget: parsing baseline %s: %v\n", baselinePath, err)
		return 1
	}

	result := CheckBaseline(current, baseline)
	if !result.OK {
		reportFailures(stderr, budget, baseline, result.Failures)
		return 1
	}

	totalOffenders := 0
	for _, vs := range current {
		totalOffenders += len(vs)
	}
	_, _ = fmt.Fprintf(stdout, "heredoc-budget: PASS (%d file(s) baselined, %d heredoc(s) over %d bytes in the current tree, no regression)\n",
		len(baseline), totalOffenders, budget)
	return 0
}

// reportFailures prints one FAIL line per regressed file (sorted for
// determinism) followed by each of its offending heredoc locations.
func reportFailures(stderr io.Writer, budget int, baseline map[string]int, failures map[string][]Violation) {
	paths := make([]string, 0, len(failures))
	for p := range failures {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		violations := failures[p]
		_, _ = fmt.Fprintf(stderr, "FAIL %s: %d heredoc(s) over %d bytes (baseline %d)\n", p, len(violations), budget, baseline[p])
		for _, v := range violations {
			_, _ = fmt.Fprintf(stderr, "  %s:%d body=%d bytes\n", v.Path, v.Line, v.Size)
		}
	}
}
