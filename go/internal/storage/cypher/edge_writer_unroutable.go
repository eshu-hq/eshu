// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded reason values for the SharedEdgeUnroutableRows counter. Keeping the
// set closed means the label stays low-cardinality; the domain, evidence
// source, and a sample intent id travel in the structured log instead.
const (
	unroutableReasonPartialBatch = "partial_batch"
	unroutableReasonWholeBatch   = "whole_batch"
)

// UnroutableRowsError reports that every row in a non-empty shared-projection
// batch failed to route to a write statement, so no edge was written.
//
// It exists so the caller can tell "this partition legitimately had nothing to
// write" (an empty batch, which returns nil) from "every row was rejected"
// (#5984). The shared projection worker completes the latest intent row only
// when WriteEdges succeeds, and completed rows are never reopened by the
// durable upsert — returning nil for a wholly-rejected batch therefore records
// missing edges as done, permanently and without a signal.
type UnroutableRowsError struct {
	Domain         string
	EvidenceSource string
	InputRows      int
	DroppedRows    int
}

// Error reports both counts because they are not always equal: a batch can hold
// control rows that carry no edge by design alongside rows that should have
// become edges and could not be routed. Saying "all N were unroutable" with the
// dropped count would understate the batch and read as though it held only the
// rejected rows.
func (e *UnroutableRowsError) Error() string {
	return fmt.Sprintf(
		"%s: no edge rows could be routed: %d of %d row(s) were unroutable "+
			"(no write statement matched; evidence_source=%s); "+
			"refusing to report success for edges that were never written",
		e.Domain, e.DroppedRows, e.InputRows, e.EvidenceSource,
	)
}

// reportUnroutableRows emits the operator-facing signal for rows a shared-edge
// write could not route: a WARN structured log naming the domain, the counts,
// and one sample intent id an operator can look up, plus the bounded
// SharedEdgeUnroutableRows counter. wholeBatch distinguishes a partial loss
// (the routable rows still wrote) from a batch that produced nothing.
func (w *EdgeWriter) reportUnroutableRows(
	ctx context.Context,
	domain string,
	evidenceSource string,
	inputRows int,
	droppedRows int,
	sampleIntentID string,
	wholeBatch bool,
) {
	reason := unroutableReasonPartialBatch
	if wholeBatch {
		reason = unroutableReasonWholeBatch
	}

	if w.Instruments != nil && w.Instruments.SharedEdgeUnroutableRows != nil {
		w.Instruments.SharedEdgeUnroutableRows.Add(ctx, int64(droppedRows), metric.WithAttributes(
			telemetry.AttrDomain(domain),
			telemetry.AttrReason(reason),
		))
	}

	if w.Logger == nil {
		return
	}
	w.Logger.Warn(
		"shared edge rows unroutable",
		"domain", domain,
		"evidence_source", evidenceSource,
		"input_rows", inputRows,
		"dropped_rows", droppedRows,
		"reason", reason,
		"sample_intent_id", sampleIntentID,
	)
}
