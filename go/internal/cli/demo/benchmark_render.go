// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
)

// ParseImageState validates the observed image-cache flag. An unrecognised
// value is rejected rather than silently treated as not-probed, which would
// turn a typo into a skipped cross-check.
func ParseImageState(v string) (ImageState, error) {
	switch strings.TrimSpace(v) {
	case "":
		return ImagesUnknown, nil
	case string(ImagesPresent):
		return ImagesPresent, nil
	case string(ImagesAbsent):
		return ImagesAbsent, nil
	default:
		return ImagesUnknown, fmt.Errorf(
			"--images %q is not %q, %q, or empty", v, ImagesPresent, ImagesAbsent,
		)
	}
}

// RenderBenchmarkVerdict writes a concise human scorecard, leading with the
// numbers the measurement lane exists to produce.
func RenderBenchmarkVerdict(w io.Writer, verdict BenchmarkVerdict) {
	header := "Demo TTFA benchmark PASSED"
	if !verdict.Pass {
		header = "Demo TTFA benchmark FAILED"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintf(w, "  mode : %s\n", quoteIfEmpty(verdict.Mode))
	_, _ = fmt.Fprintf(w, "  TTFA : %s\n", millisText(verdict.TTFAMillis))
	if verdict.TargetMillis > 0 {
		_, _ = fmt.Fprintf(w, "  target: %s\n", millisText(verdict.TargetMillis))
	}
	for _, phase := range requiredPhases {
		if ms, ok := verdict.PhaseMillis[phase]; ok {
			_, _ = fmt.Fprintf(w, "    %-13s %s\n", phase, millisText(ms))
		}
	}
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	for _, c := range verdict.Criteria {
		req := " "
		if c.Required {
			req = "*"
		}
		_, _ = fmt.Fprintf(w, "  %s %s %s: %s\n", firstrunbench.Marker(c.Status), req, c.Name, c.Detail)
	}
	_, _ = fmt.Fprintln(w, "  (* = required; failure rejects the run)")
}

// millisText renders a millisecond count as a human duration.
func millisText(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).Round(time.Millisecond).String()
}
