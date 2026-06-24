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
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	var findingFact, markerFact *facts.Envelope
	for i := range envelopes {
		switch envelopes[i].FactKind {
		case facts.CodeInterprocEvidenceFactKind:
			if findingFact == nil {
				findingFact = &envelopes[i]
			}
		case facts.CodeDataflowScannedFactKind:
			if markerFact == nil {
				markerFact = &envelopes[i]
			}
		}
	}

	trigger := findingFact
	reason := "cross-function value-flow evidence observed"
	if trigger == nil {
		trigger = markerFact
		reason = "value-flow gate scanned; reconcile cross-function evidence"
	}
	if trigger == nil {
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
