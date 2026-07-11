// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// baselineHeader is written verbatim at the top of every generated baseline
// file. It explains the burn-down rule to anyone reading the file cold.
const baselineHeader = `# heredoc-budget-baseline.txt
#
# Burn-down baseline for scripts/**/*.sh heredoc bodies over the byte budget
# (#5074). Homebrew bash >= 5.1 writes an entire heredoc body to a pipe
# before forking the reader; macOS's 512-byte pipe buffer means any body
# strictly between 512 bytes and the ~64 KB pipe-buffer ceiling deadlocks
# under that bash even though the same script runs fine under macOS's stock
# /bin/bash 3.2. The heredoc-budget gate (go/cmd/heredoc-budget) fails a
# change that adds a NEW file with an over-budget heredoc, or that INCREASES
# an already-baselined file's over-budget heredoc count. A file's count may
# stay the same or decrease (burn-down progress) without failing the gate.
#
# Regenerate with:
#   cd go && go run ./cmd/heredoc-budget -baseline ../scripts/heredoc-budget-baseline.txt -update
#
# <relative/path> <count-of-heredocs-over-budget>
`

// ParseBaseline reads the baseline file format: blank lines and lines
// starting with "#" are ignored; every other line must be "<path> <count>".
func ParseBaseline(r io.Reader) (map[string]int, error) {
	counts := make(map[string]int)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("baseline line %d: expected \"<path> <count>\", got %q", lineNo, line)
		}
		count, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("baseline line %d: invalid count %q: %w", lineNo, fields[1], err)
		}
		counts[fields[0]] = count
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}
	return counts, nil
}

// RenderBaseline renders counts deterministically: the header comment,
// followed by one "<path> <count>" line per entry sorted by path.
// Zero-count entries are omitted — the baseline only tracks files with an
// outstanding over-budget heredoc, so burning a file down to zero and
// regenerating drops it from the file entirely.
func RenderBaseline(counts map[string]int) string {
	paths := make([]string, 0, len(counts))
	for p, c := range counts {
		if c > 0 {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	var b strings.Builder
	b.WriteString(baselineHeader)
	for _, p := range paths {
		fmt.Fprintf(&b, "%s %d\n", p, counts[p])
	}
	return b.String()
}

// CheckResult is the outcome of comparing current scan results to a
// baseline.
type CheckResult struct {
	// OK is true when no file regressed against the baseline.
	OK bool
	// Failures holds every current violation for each file that regressed,
	// keyed by repo-relative path.
	Failures map[string][]Violation
}

// CheckBaseline compares current per-file violations against baseline
// counts. A file fails when either:
//   - it has 1+ violations and is not present in baseline (a new offender), or
//   - it is present in baseline and its current violation count exceeds the
//     baselined count (a regression).
//
// A file whose count stayed the same or decreased passes: burn-down
// progress (baselined count going down, including to zero) is the expected,
// encouraged path and must never fail the gate.
func CheckBaseline(current map[string][]Violation, baseline map[string]int) CheckResult {
	result := CheckResult{OK: true, Failures: make(map[string][]Violation)}
	for path, violations := range current {
		count := len(violations)
		if count == 0 {
			continue
		}
		baselineCount, known := baseline[path]
		if !known || count > baselineCount {
			result.OK = false
			result.Failures[path] = violations
		}
	}
	return result
}
