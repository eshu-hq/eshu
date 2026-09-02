// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinterprocevidence

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildCodeInterprocEvidenceReducerIntent queues one cross-function evidence
// materialization intent per scope generation. It fires both when a
// code_interproc_evidence finding is present and when only the
// code_dataflow_scanned marker is present (the value-flow gate ran but produced
// no cross-function findings this generation). The marker case lets the reducer
// retract stale TAINT_FLOWS_TO edges when a prior generation's findings are
// edited away. A finding is preferred as the intent's provenance; the marker is
// the fallback trigger. Summary-driven fixpoint projection is triggered after
// the function-summary handler persists durable summaries, sources, and graph
// ids so it cannot retract direct interproc evidence under the same scope.
//
// The source-system label is the trimmed CollectorKind alone — a single tier,
// unlike the shared two-tier projectorintent.SourceSystem, which would prefer
// SourceRef.SourceSystem when set. Substituting the shared helper would change
// the label for any generation whose trigger fact carries a source-ref identity.
func BuildCodeInterprocEvidenceReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	trigger, reason, ok := codeInterprocEvidenceTrigger(lookup)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainCodeInterprocEvidence,
		EntityKey:    "code_interproc_evidence:" + scopeID,
		Reason:       reason,
		FactID:       trigger.FactID,
		SourceSystem: strings.TrimSpace(trigger.CollectorKind),
	}, true
}

// codeInterprocEvidenceTrigger resolves the anchor fact for
// BuildCodeInterprocEvidenceReducerIntent: a code_interproc_evidence finding
// when present, else the code_dataflow_scanned marker as a
// retraction-reconcile fallback. The two kinds are looked up independently —
// this domain does not need cross-kind original-order merging because a
// finding always outranks the marker regardless of which appears earlier in
// the generation.
func codeInterprocEvidenceTrigger(lookup projectorintent.FactLookup) (facts.Envelope, string, bool) {
	if finding, ok := lookup.FirstOfKind(facts.CodeInterprocEvidenceFactKind); ok {
		return finding, "cross-function value-flow evidence observed", true
	}
	if marker, ok := lookup.FirstOfKind(facts.CodeDataflowScannedFactKind); ok {
		return marker, "value-flow gate scanned; reconcile cross-function evidence", true
	}
	return facts.Envelope{}, "", false
}
