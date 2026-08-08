// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// pipelineStateQuerier reads the producer-side state behind a missing edge.
//
// Deliberately one method. The graph-side diagnosis answers "what does the
// graph look like"; this answers the one Postgres question that graph reads
// cannot: did the work that writes this edge actually finish?
type pipelineStateQuerier interface {
	// NonTerminalWorkItems returns work items that have not reached a terminal
	// success, grouped the same way the drain residual is grouped so the two
	// messages read alike.
	NonTerminalWorkItems(ctx context.Context) ([]residualRow, error)
}

// diagnoseZeroCorrelationPipeline reports whether the producers behind a
// zero-valued correlation ran to completion.
//
// The graph-side diagnosis (graph_zero_correlation_diagnosis.go) separates
// three causes of a zero: the edge type is absent entirely, it exists between
// other labels, or the read is unstable. #5717 excluded all three and the
// answer still was not in the output. Both endpoint nodes passed their own
// assertions, the edge type was absent, the retry agreed — and none of that
// says whether the domain that writes the edge ever got that far.
//
// That question lives in Postgres. What this can honestly answer is narrower
// than the question, and the wording matters:
//
//   - work items outstanding: name the domains. The gate has NO
//     relationship-to-domain registry, so it cannot say whether those domains
//     are this edge's producer — only that they exist and should be ruled out.
//   - nothing outstanding: a stalled queue is not the explanation. This is NOT
//     evidence the producer ran. The read is the global residual, and a producer
//     that never enqueued a single item leaves exactly the same empty result as
//     one that finished cleanly.
//
// An earlier version of this claimed "the producers ran" on an empty residual
// and "the missing edge is downstream of this" on a non-empty one. Both assert
// a producer link the query cannot establish, and the first turns a
// never-enqueued producer into a clean bill of health — the absence-as-success
// failure this file was written to attack (codex review of #5976).
//
// Advisory and failure-path only, like its graph sibling: it runs after a
// failure is already decided, and a read error degrades the message rather than
// the verdict.
func diagnoseZeroCorrelationPipeline(ctx context.Context, q pipelineStateQuerier, relationship string) string {
	if q == nil {
		return ""
	}

	rows, err := q.NonTerminalWorkItems(ctx)
	if err != nil {
		return fmt.Sprintf("pipeline state near %s: UNREADABLE (%v) — cannot say whether any work is outstanding", relationship, err)
	}

	if len(rows) == 0 {
		return fmt.Sprintf(
			"pipeline state near %s: no outstanding work anywhere — a stalled queue is not the explanation. This does NOT show this edge's producer ran: the read is the global residual, and a producer that never enqueued anything leaves the same empty result as one that finished",
			relationship,
		)
	}

	// Group by domain so one unfinished domain reads as one line regardless of
	// how many statuses it is spread across.
	byDomain := map[string][]string{}
	totals := map[string]int64{}
	for _, row := range rows {
		detail := row.Status
		if row.FailureClass != "" {
			detail += "/" + row.FailureClass
		}
		byDomain[row.Domain] = append(byDomain[row.Domain], fmt.Sprintf("%s=%d", detail, row.Count))
		totals[row.Domain] += row.Count
	}

	domains := make([]string, 0, len(byDomain))
	for d := range byDomain {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	parts := make([]string, 0, len(domains))
	for _, d := range domains {
		details := byDomain[d]
		sort.Strings(details)
		parts = append(parts, fmt.Sprintf("%s[%s]", d, strings.Join(details, " ")))
	}

	return fmt.Sprintf(
		"pipeline state near %s: %d domain(s) have outstanding work — %s. These may or may not include this edge's producer (the gate has no relationship-to-domain registry), so rule them out before suspecting the edge writer",
		relationship, len(domains), strings.Join(parts, " "),
	)
}

// Compile-time proof that the production querier satisfies this interface.
// Without it, a signature drift on either side would leave runGraph silently
// passing nil and the diagnosis would stop firing with every test still green —
// the exact dead-code shape this file exists to prevent.
var _ pipelineStateQuerier = (*sqlDrainQuerier)(nil)

// emitZeroCorrelationDiagnostics adds both halves of a zero-correlation
// explanation to the report: what the graph looks like, and whether the
// producer behind the edge ever finished.
//
// They live together because they are one answer split across two stores.
// Reading either alone is what left #5717 stalled -- the graph half excluded
// every graph-side cause and the Postgres half was never asked.
//
// Both findings are advisory. This runs after a failure is already decided and
// must never change a verdict.
func emitZeroCorrelationDiagnostics(
	ctx context.Context,
	c graphCounter,
	pipeline pipelineStateQuerier,
	rc RequiredCorrelation,
	r *Report,
) {
	r.Add(Finding{
		Phase:    "graph",
		Check:    rc.ID + "/diagnosis",
		OK:       true,
		Required: false,
		Detail:   diagnoseZeroCorrelation(ctx, c, rc),
	})
	if detail := diagnoseZeroCorrelationPipeline(ctx, pipeline, rc.Relationship); detail != "" {
		r.Add(Finding{
			Phase:    "graph",
			Check:    rc.ID + "/pipeline",
			OK:       true,
			Required: false,
			Detail:   detail,
		})
	}
}
