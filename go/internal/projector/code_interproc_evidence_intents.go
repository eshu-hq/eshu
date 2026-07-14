// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeInterprocEvidenceReducerIntent queues one cross-function evidence
// materialization intent per scope generation. It fires both when a
// code_interproc_evidence finding is present and when only the
// code_dataflow_scanned marker is present (the value-flow gate ran but produced
// no cross-function findings this generation). The marker case lets the reducer
// retract stale TAINT_FLOWS_TO edges when a prior generation's findings are
// edited away. A finding is preferred as the intent's provenance; the marker is
// the fallback trigger. Summary-driven fixpoint projection is triggered after
// the function-summary handler persists durable summaries, sources, and graph
// ids so it cannot retract direct interproc evidence under the same scope.
func buildCodeInterprocEvidenceReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	trigger, reason, ok := codeInterprocEvidenceTrigger(index)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainCodeInterprocEvidence,
		EntityKey:    "code_interproc_evidence:" + scopeValue.ScopeID,
		Reason:       reason,
		FactID:       trigger.FactID,
		SourceSystem: strings.TrimSpace(trigger.CollectorKind),
	}, true
}

// codeInterprocEvidenceTrigger resolves the anchor fact for
// buildCodeInterprocEvidenceReducerIntent: a code_interproc_evidence finding
// when present, else the code_dataflow_scanned marker as a
// retraction-reconcile fallback. The two kinds are looked up independently —
// this domain does not need cross-kind original-order merging because a
// finding always outranks the marker regardless of which appears earlier in
// the generation.
func codeInterprocEvidenceTrigger(index *reducerIntentFactIndex) (facts.Envelope, string, bool) {
	if finding, ok := index.firstOfKind(facts.CodeInterprocEvidenceFactKind); ok {
		return finding, "cross-function value-flow evidence observed", true
	}
	if marker, ok := index.firstOfKind(facts.CodeDataflowScannedFactKind); ok {
		return marker, "value-flow gate scanned; reconcile cross-function evidence", true
	}
	return facts.Envelope{}, "", false
}
