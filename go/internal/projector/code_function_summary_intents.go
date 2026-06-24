// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeFunctionSummaryReducerIntent queues one function-summary persistence
// intent per scope generation when either summary facts or the full-snapshot
// value-flow scan marker are present. Summary facts refresh changed functions;
// the full marker additionally lets the reducer replace the repo snapshot and
// prune summaries deleted or renamed out of the latest complete scan.
func buildCodeFunctionSummaryReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	var summaryFact, markerFact *facts.Envelope
	for i := range envelopes {
		switch envelopes[i].FactKind {
		case facts.CodeFunctionSummaryFactKind:
			if summaryFact == nil {
				summaryFact = &envelopes[i]
			}
		case facts.CodeDataflowScannedFactKind:
			if markerFact == nil {
				markerFact = &envelopes[i]
			}
		}
	}

	trigger := summaryFact
	reason := "value-flow function summaries observed"
	if trigger == nil {
		trigger = markerFact
		reason = "value-flow gate scanned; reconcile function summaries"
	}
	if trigger == nil {
		return ReducerIntent{}, false
	}
	payload := map[string]any{}
	repoID, _ := payloadString(trigger.Payload, "repo_id")
	if repoID == "" && markerFact != nil {
		repoID, _ = payloadString(markerFact.Payload, "repo_id")
	}
	if repoID != "" {
		payload["repo_id"] = repoID
	}
	if markerFact != nil {
		payload["full_snapshot"] = true
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainCodeFunctionSummary,
		EntityKey:    "code_function_summary:" + scopeValue.ScopeID,
		Reason:       reason,
		FactID:       trigger.FactID,
		SourceSystem: strings.TrimSpace(trigger.CollectorKind),
		Payload:      payload,
	}, true
}
