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
// That question lives in Postgres, and answering it splits the remaining space
// cleanly:
//
//   - work items outstanding: the missing edge is a CONSEQUENCE. Fix the queue,
//     not the writer. The domains are named so the reader knows which.
//   - everything succeeded: the producer ran and the edge is still absent, so
//     the fault is in the write path — resolution, the join, or the query — and
//     that is a different owner and a different fix.
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
		return fmt.Sprintf("pipeline state for %s: UNREADABLE (%v) — cannot say whether the producers finished", relationship, err)
	}

	if len(rows) == 0 {
		return fmt.Sprintf(
			"pipeline state for %s: every work item reached a terminal success, so the producers ran and the edge is still absent — look at the write path (resolution, join, or edge writer), not the queue",
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
		"pipeline state for %s: %d domain(s) never reached a terminal success — %s. The missing edge is downstream of this, so fix the outstanding work before suspecting the edge writer",
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
