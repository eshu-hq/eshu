// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codetaintevidence

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildCodeTaintEvidenceReducerIntent queues one taint-evidence materialization
// intent per scope generation. It fires both when a code_taint_evidence finding
// is present AND when only the code_dataflow_scanned marker is present (the
// value-flow gate ran but produced no taint findings this generation). The marker
// case is what lets the reducer retract stale CodeTaintEvidence nodes when a prior
// generation's findings are edited away — without it an empty finding set queues
// no intent and the old evidence leaks (#2919). A finding is preferred as the
// intent's provenance; the marker is the fallback trigger.
//
// The source-system label is the trimmed CollectorKind alone — a single tier,
// unlike the shared two-tier projectorintent.SourceSystem, which would prefer
// SourceRef.SourceSystem when set. Substituting the shared helper would change
// the label for any generation whose trigger fact carries a source-ref identity.
func BuildCodeTaintEvidenceReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	trigger, reason, ok := codeTaintEvidenceTrigger(lookup)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainCodeTaintEvidence,
		EntityKey:    "code_taint_evidence:" + scopeID,
		Reason:       reason,
		FactID:       trigger.FactID,
		SourceSystem: strings.TrimSpace(trigger.CollectorKind),
	}, true
}

// codeTaintEvidenceTrigger resolves the anchor fact for
// BuildCodeTaintEvidenceReducerIntent: a code_taint_evidence finding when
// present, else the code_dataflow_scanned marker as a retraction-reconcile
// fallback. The two kinds are looked up independently — this domain does not
// need cross-kind original-order merging because a finding always outranks
// the marker regardless of which appears earlier in the generation.
func codeTaintEvidenceTrigger(lookup projectorintent.FactLookup) (facts.Envelope, string, bool) {
	if finding, ok := lookup.FirstOfKind(facts.CodeTaintEvidenceFactKind); ok {
		return finding, "value-flow taint evidence observed", true
	}
	if marker, ok := lookup.FirstOfKind(facts.CodeDataflowScannedFactKind); ok {
		return marker, "value-flow gate scanned; reconcile taint evidence", true
	}
	return facts.Envelope{}, "", false
}
