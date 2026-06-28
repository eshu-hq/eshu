// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"fmt"
	"io"
	"sort"
)

// Finding is a single gate assertion outcome. Required findings that fail cause
// a non-zero exit; advisory findings are reported but never fail the gate. The
// advisory tier exists so the minimal 5-repo gate can surface the 20-repo
// count-tolerance diff for visibility without blocking on ranges calibrated for
// a different corpus size.
type Finding struct {
	// Phase groups findings: "drains", "graph", "query", "timing".
	Phase string
	// Check is a short stable identifier, e.g. "fact_work_items_residual".
	Check string
	// OK is the pass/fail outcome.
	OK bool
	// Required marks a finding that fails the gate when not OK.
	Required bool
	// Detail is a human-facing one-line explanation with the observed value.
	Detail string
}

// Report accumulates findings across all gate phases.
type Report struct {
	Findings []Finding
}

// Add appends a finding.
func (r *Report) Add(f Finding) {
	r.Findings = append(r.Findings, f)
}

// AddCheck is a convenience for appending a finding from a boolean outcome.
func (r *Report) AddCheck(phase, check string, ok, required bool, detail string) {
	r.Add(Finding{Phase: phase, Check: check, OK: ok, Required: required, Detail: detail})
}

// Failed reports whether any required finding did not pass. An empty report is
// treated as a failure: a gate that asserted nothing has not proven anything.
func (r *Report) Failed() bool {
	if len(r.Findings) == 0 {
		return true
	}
	for _, f := range r.Findings {
		if f.Required && !f.OK {
			return true
		}
	}
	return false
}

// Write renders the report grouped by phase. Required failures sort first within
// each phase so the most important lines are easy to find in CI logs.
func (r *Report) Write(w io.Writer) {
	byPhase := map[string][]Finding{}
	order := []string{}
	for _, f := range r.Findings {
		if _, seen := byPhase[f.Phase]; !seen {
			order = append(order, f.Phase)
		}
		byPhase[f.Phase] = append(byPhase[f.Phase], f)
	}

	var pass, requiredFail, advisoryFail int
	for _, phase := range order {
		_, _ = fmt.Fprintf(w, "\n== %s ==\n", phase)
		fs := byPhase[phase]
		sort.SliceStable(fs, func(i, j int) bool {
			// Required failures first, then advisory failures, then passes.
			rank := func(f Finding) int {
				switch {
				case f.Required && !f.OK:
					return 0
				case !f.OK:
					return 1
				default:
					return 2
				}
			}
			return rank(fs[i]) < rank(fs[j])
		})
		for _, f := range fs {
			mark := "PASS"
			switch {
			case f.OK:
				pass++
			case f.Required:
				mark = "FAIL"
				requiredFail++
			default:
				mark = "WARN"
				advisoryFail++
			}
			_, _ = fmt.Fprintf(w, "  [%s] %s: %s\n", mark, f.Check, f.Detail)
		}
	}
	_, _ = fmt.Fprintf(w, "\nsummary: %d pass, %d required-fail, %d advisory-warn\n", pass, requiredFail, advisoryFail)
}
