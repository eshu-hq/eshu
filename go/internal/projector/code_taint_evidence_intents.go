// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeTaintEvidenceReducerIntent queues one taint-evidence materialization
// intent per scope generation. It fires both when a code_taint_evidence finding
// is present AND when only the code_dataflow_scanned marker is present (the
// value-flow gate ran but produced no taint findings this generation). The marker
// case is what lets the reducer retract stale CodeTaintEvidence nodes when a prior
// generation's findings are edited away — without it an empty finding set queues
// no intent and the old evidence leaks (#2919). A finding is preferred as the
// intent's provenance; the marker is the fallback trigger.
func buildCodeTaintEvidenceReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	trigger, reason, ok := codeTaintEvidenceTrigger(index)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainCodeTaintEvidence,
		EntityKey:    "code_taint_evidence:" + scopeValue.ScopeID,
		Reason:       reason,
		FactID:       trigger.FactID,
		SourceSystem: strings.TrimSpace(trigger.CollectorKind),
	}, true
}

// codeTaintEvidenceTrigger resolves the anchor fact for
// buildCodeTaintEvidenceReducerIntent: a code_taint_evidence finding when
// present, else the code_dataflow_scanned marker as a retraction-reconcile
// fallback. The two kinds are looked up independently — this domain does not
// need cross-kind original-order merging because a finding always outranks
// the marker regardless of which appears earlier in the generation.
func codeTaintEvidenceTrigger(index *reducerIntentFactIndex) (facts.Envelope, string, bool) {
	if finding, ok := index.firstOfKind(facts.CodeTaintEvidenceFactKind); ok {
		return finding, "value-flow taint evidence observed", true
	}
	if marker, ok := index.firstOfKind(facts.CodeDataflowScannedFactKind); ok {
		return marker, "value-flow gate scanned; reconcile taint evidence", true
	}
	return facts.Envelope{}, "", false
}
