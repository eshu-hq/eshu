// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"strings"
)

// report prints the findings an operator needs and returns the exit status.
func report(w io.Writer, res resolution, findings []finding, checked, nPaths, historyWaived int) int {
	_, _ = fmt.Fprintf(w, "dirgate: mode=%s base=%s head-tree=%s trailer-range=%s\n",
		res.mode, short(res.baseCommit), short(res.headTree), res.revRange)
	_, _ = fmt.Fprintf(w, "dirgate: waived on history reachable from base: W=%d\n", historyWaived)
	for _, n := range res.notes {
		_, _ = fmt.Fprintf(w, "dirgate: NOTE %s\n", n)
	}
	var reds, notes int
	for _, f := range findings {
		msg, sev := f.msg, f.sev
		switch {
		case sev == sevRed && res.pending && f.kind.needsMessage():
			msg = "NOTE " + strings.TrimPrefix(msg, "RED ") +
				"\n  (pre-commit cannot see this commit's message; pre-push and CI enforce it)"
			sev = sevNote
		}
		if sev == sevRed {
			reds++
		} else {
			notes++
		}
		_, _ = fmt.Fprintf(w, "dirgate: %s\n", msg)
	}
	scope := ""
	if res.mode == modePreCommit {
		scope = fmt.Sprintf(" from %d path(s)", nPaths)
	}
	_, _ = fmt.Fprintf(w, "dirgate: evaluated %d director%s%s: %d RED, %d NOTE\n",
		checked, plural(checked), scope, reds, notes)
	if reds > 0 {
		return exitRed
	}
	return exitGreen
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
