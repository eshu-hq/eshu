// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded reason values for the SharedEdgeUnroutableRows counter. Keeping the
// set closed means the label stays low-cardinality; the domain, evidence
// source, and a sample intent id travel in the structured log instead.
const (
	unroutableReasonPartialBatch = "partial_batch"
	unroutableReasonWholeBatch   = "whole_batch"
)

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

// unroutableReasonForRow classifies WHY a row could not be routed, so an
// operator can tell a genuine data loss from version skew.
//
// The distinction is not cosmetic. A row missing its MATCH identifiers can
// never become an edge under any writer version, and the loss is real. A row
// whose relationship type has no statement in THIS binary is well formed and
// would route once the writer catches up — during a rolling upgrade a newer
// producer emits types an older writer has never heard of. Recording both as
// the same thing would either hide real losses in deploy noise or page someone
// about a rollout that is working as intended.
func unroutableReasonForRow(domain string, row reducer.SharedProjectionIntentRow) string {
	if domain != reducer.DomainRepoDependency {
		return reducer.UnroutableReasonMissingRequiredField
	}
	// repo_dependency is the only domain today whose rejection can mean "this
	// writer has no statement for that type" rather than "the payload is
	// incomplete": it looks the relationship type up in a table
	// (batchCanonicalTypedRepoRelationshipUpsertCypher). Every other domain
	// rejects only on empty required fields.
	relationshipType := payloadString(row.Payload, "relationship_type")
	if relationshipType == "" || relationshipType == string(edgetype.DependsOn) {
		return reducer.UnroutableReasonMissingRequiredField
	}
	if payloadString(row.Payload, "repo_id") == "" || payloadString(row.Payload, "target_repo_id") == "" {
		return reducer.UnroutableReasonMissingRequiredField
	}
	if _, ok := batchCanonicalTypedRepoRelationshipUpsertCypher(relationshipType); !ok {
		return reducer.UnroutableReasonNoStatementForType
	}
	return reducer.UnroutableReasonMissingRequiredField
}
