// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidpacket

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
)

// ReadBenchmark returns the raw bytes of a dogfood benchmark artifact. It reads
// path when path has non-space content, and stdin otherwise; callers that want
// stdin pass the empty string. It does not parse or validate the bytes -- that
// is packetdogfood.ParseBenchmark's job.
func ReadBenchmark(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read dogfood benchmark from stdin: %w", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local benchmark artifact path, not an HTTP request param //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read dogfood benchmark file %q: %w", path, err)
	}
	return raw, nil
}

// RenderVerdict returns the operator-facing text report for a scored verdict:
// a PASSED/FAILED header, the run identity, task and family coverage, then one
// line per criterion. The returned string ends in a newline, so a caller writes
// it with fmt.Fprint rather than fmt.Fprintln.
func RenderVerdict(verdict packetdogfood.Verdict) string {
	var b strings.Builder
	header := "Evidence-packet dogfood PASSED"
	if !verdict.Pass {
		header = "Evidence-packet dogfood FAILED"
	}
	_, _ = fmt.Fprintln(&b, header)
	_, _ = fmt.Fprintf(&b, "  run     : %s (%s)\n", quoteIfEmpty(verdict.RunID), verdict.RunKind)
	_, _ = fmt.Fprintf(&b, "  tasks   : %d\n", verdict.TaskCount)
	_, _ = fmt.Fprintf(&b, "  families: %s\n", strings.Join(verdict.Families, ", "))
	_, _ = fmt.Fprintln(&b, strings.Repeat("-", 44))
	for _, criterion := range verdict.Criteria {
		_, _ = fmt.Fprintf(&b, "  %s %s: %s\n", marker(criterion.Status), criterion.Name, criterion.Detail)
	}
	return b.String()
}

// FailureSummary joins every failed criterion into one semicolon-separated
// line for an error message. It returns "unknown failure" when no criterion
// carries packetdogfood.CriterionFail, which is what a caller sees if it asks
// for a summary of a verdict that did not actually fail.
func FailureSummary(verdict packetdogfood.Verdict) string {
	var failures []string
	for _, criterion := range verdict.Criteria {
		if criterion.Status == packetdogfood.CriterionFail {
			failures = append(failures, fmt.Sprintf("%s: %s", criterion.Name, criterion.Detail))
		}
	}
	if len(failures) == 0 {
		return "unknown failure"
	}
	return strings.Join(failures, "; ")
}

// marker renders the fixed-width status glyph that opens a criterion line.
// packetdogfood.Score never emits CriterionSkip today, so the "[--]" default
// is reachable only from a hand-built Verdict.
func marker(status packetdogfood.CriterionStatus) string {
	switch status {
	case packetdogfood.CriterionPass:
		return "[ok]"
	case packetdogfood.CriterionFail:
		return "[!!]"
	default:
		return "[--]"
	}
}

// quoteIfEmpty substitutes the placeholder "<repo>" for a value that is empty
// or all spaces. Its body is a verbatim copy of the helper in go/cmd/eshu's
// first_run.go, which this package cannot import because go/cmd/eshu is
// `package main`. The placeholder text reads oddly here -- RenderVerdict's only
// call site passes a run id, not a repo -- but issue #6059 is a
// behavior-preserving move, so the string is carried across unchanged rather
// than corrected.
func quoteIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<repo>"
	}
	return value
}
