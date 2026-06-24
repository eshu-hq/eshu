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
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	var findingFact, markerFact *facts.Envelope
	for i := range envelopes {
		switch envelopes[i].FactKind {
		case facts.CodeTaintEvidenceFactKind:
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
	reason := "value-flow taint evidence observed"
	if trigger == nil {
		trigger = markerFact
		reason = "value-flow gate scanned; reconcile taint evidence"
	}
	if trigger == nil {
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
