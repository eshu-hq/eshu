// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

import (
	"fmt"
	"io"
	"strings"
)

// RenderVerdict writes a concise human scorecard with a stable marker per
// criterion so the operator can see exactly which guard failed.
func RenderVerdict(w io.Writer, verdict Verdict) {
	header := "First-answer benchmark PASSED"
	if !verdict.Pass {
		header = "First-answer benchmark FAILED"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintf(w, "  path : %s\n", quoteIfEmpty(verdict.Path))
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	for _, c := range verdict.Criteria {
		req := " "
		if c.Required {
			req = "*"
		}
		_, _ = fmt.Fprintf(w, "  %s %s %s: %s\n", Marker(c.Status), req, c.Name, c.Detail)
	}
	_, _ = fmt.Fprintln(w, "  (* = required; failure rejects the run)")
}

// Marker maps a criterion status to a stable ASCII marker. The demo-benchmark
// family renders its scorecard with the same glyphs, so this stays exported.
func Marker(status CriterionStatus) string {
	switch status {
	case CriterionPass:
		return "[ok]"
	case CriterionFail:
		return "[!!]"
	case CriterionNotMeasured:
		return "[--]"
	default:
		return "[--]"
	}
}

// quoteIfEmpty renders a placeholder for an empty value so the scorecard line
// stays copy-pasteable. It is a verbatim copy of the helper in
// go/cmd/eshu/first_run.go, kept local because package main cannot be
// imported; do not export or import it from anywhere. The name matches the
// copies in internal/cli/evidpacket and internal/cli/answerqualityscorecard,
// so the four verbatim copies are greppable as one family.
func quoteIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<repo>"
	}
	return value
}
