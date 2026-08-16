// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

import (
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
)

// RenderVerdict writes a concise human scorecard with a stable marker per
// criterion so the operator can see exactly which guard failed.
func RenderVerdict(w io.Writer, verdict Verdict) {
	header := "First-answer benchmark PASSED"
	if !verdict.Pass {
		header = "First-answer benchmark FAILED"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintf(w, "  path : %s\n", firstrun.QuoteIfEmpty(verdict.Path))
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
