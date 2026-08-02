// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"
)

// diagnoseZeroCorrelation gathers, while the graph is still up, the reads that
// separate the three causes of a correlation reading zero.
//
// The bare "count=0, want >= 1" a zero produces today is true and nearly
// useless: the edge was never written, or it was written between endpoints
// carrying other labels, or the read came back wrong. Telling those apart after
// the fact is usually impossible, because the gate tears its stack down on exit
// and the evidence goes with it — reconstructing one such failure cost most of a
// day and ended in a wrong conclusion drawn from a different run's surviving
// stack (#5717).
//
// Three extra reads, only on a zero, none of which can change the verdict:
//
//   - the untyped edge count, MATCH ()-[r:REL]->(): separates "no such edge
//     anywhere" from "exists, wrong endpoint labels".
//   - node counts for both endpoint labels: an empty label can never satisfy the
//     correlation regardless of the edge, and points at the node writer rather
//     than the edge writer.
//   - one retry of the same labeled count: a differing retry is read
//     instability, which on this graph backend is a documented failure mode
//     rather than a theoretical one.
//
// Returns a one-line detail. Read errors are reported inline rather than
// returned: a diagnosis that fails the gate it is trying to explain would be
// worse than no diagnosis.
func diagnoseZeroCorrelation(ctx context.Context, c graphCounter, rc RequiredCorrelation) string {
	var parts []string

	untyped, err := c.CountEdges(ctx, rc.Relationship)
	switch {
	case err != nil:
		parts = append(parts, fmt.Sprintf("untyped=ERR(%v)", err))
	default:
		parts = append(parts, fmt.Sprintf("untyped=%d", untyped))
	}

	labels := []string{rc.FromLabel}
	if rc.ToLabel != rc.FromLabel {
		labels = append(labels, rc.ToLabel)
	}
	emptyLabel := ""
	for _, label := range labels {
		n, nErr := c.CountNodes(ctx, label)
		if nErr != nil {
			parts = append(parts, fmt.Sprintf("%s=ERR(%v)", label, nErr))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", label, n))
		if n == 0 && emptyLabel == "" {
			emptyLabel = label
		}
	}

	// The retry must be the SAME query the assertion ran. When the assertion is
	// evidence-filtered, retrying the unfiltered count reads a shared
	// relationship's other evidence kinds, so a stable evidence-kind regression
	// would come back nonzero and be reported as read instability — the one
	// conclusion that sends the reader looking at the backend instead of the
	// writer.
	narrowed := len(rc.EvidenceKinds) > 0
	var retry int64
	var retryErr error
	if narrowed {
		retry, retryErr = c.CountCorrelationWithEvidence(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel, rc.EvidenceKinds)
	} else {
		retry, retryErr = c.CountCorrelation(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel)
	}
	if retryErr != nil {
		parts = append(parts, fmt.Sprintf("retry=ERR(%v)", retryErr))
	} else {
		parts = append(parts, fmt.Sprintf("retry=%d", retry))
	}

	// For a narrowed assertion the unfiltered count is the read that separates
	// "the shape is missing" from "the shape is there but carries different
	// evidence" — a distinction with different owners and different fixes.
	var unfiltered int64
	var unfilteredErr error
	if narrowed {
		unfiltered, unfilteredErr = c.CountCorrelation(ctx, rc.FromLabel, rc.Relationship, rc.ToLabel)
		if unfilteredErr != nil {
			parts = append(parts, fmt.Sprintf("unfiltered=ERR(%v)", unfilteredErr))
		} else {
			parts = append(parts, fmt.Sprintf("unfiltered=%d", unfiltered))
		}
	}

	cause := ""
	switch {
	case retryErr == nil && retry > 0:
		cause = "read instability: the same query returned a different count on retry"
	case narrowed && unfilteredErr == nil && unfiltered > 0:
		cause = fmt.Sprintf("the correlation exists but no path carries the asserted evidence kind(s) %s", strings.Join(rc.EvidenceKinds, ","))
	case emptyLabel != "":
		cause = fmt.Sprintf("endpoint label %s has no nodes, so this correlation can never match", emptyLabel)
	case err == nil && untyped == 0:
		cause = "no edge of this type exists in the graph — nothing was written"
	case err == nil && untyped > 0 && narrowed:
		cause = "the edge exists but neither its endpoint labels nor its evidence kinds match the assertion"
	case err == nil && untyped > 0:
		cause = "the edge exists but its endpoint labels do not match the assertion"
	default:
		cause = "inconclusive: a diagnostic read failed"
	}

	return strings.Join(parts, " ") + " — " + cause
}
