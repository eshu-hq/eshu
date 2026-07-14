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
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	// The two candidate kinds are looked up independently, not merged in
	// original order: a summary fact always outranks the marker as the
	// trigger regardless of which appears earlier in the generation, and the
	// payload construction below needs both facts' repo IDs, not just the
	// winning trigger's.
	summaryFact, hasSummaryFact := index.firstOfKind(facts.CodeFunctionSummaryFactKind)
	markerFact, hasMarkerFact := index.firstOfKind(facts.CodeDataflowScannedFactKind)

	var trigger *facts.Envelope
	reason := "value-flow function summaries observed"
	if hasSummaryFact {
		trigger = &summaryFact
	} else if hasMarkerFact {
		trigger = &markerFact
		reason = "value-flow gate scanned; reconcile function summaries"
	}
	if trigger == nil {
		return ReducerIntent{}, false
	}
	payload := map[string]any{}
	repoID := codeFunctionSummaryTriggerRepoID(trigger)
	// hasMarkerFact && hasSummaryFact reproduces the original pointer check
	// "markerFact != nil && markerFact != trigger": trigger only equals the
	// marker when no summary fact is present, so a distinct marker fallback
	// exists exactly when both facts are present and the summary won as
	// trigger.
	if repoID == "" && hasMarkerFact && hasSummaryFact {
		repoID = codeFunctionSummaryTriggerRepoID(&markerFact)
	}
	if repoID != "" {
		payload["repo_id"] = repoID
	}
	if hasMarkerFact {
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

func codeFunctionSummaryTriggerRepoID(trigger *facts.Envelope) string {
	if trigger == nil {
		return ""
	}
	switch trigger.FactKind {
	case facts.CodeFunctionSummaryFactKind:
		summary, err := decodeCodeFunctionSummary(*trigger)
		if err != nil {
			return ""
		}
		return repoIDFromFunctionID(summary.FunctionID)
	case facts.CodeDataflowScannedFactKind:
		scanned, err := decodeCodeDataflowScanned(*trigger)
		if err != nil {
			return ""
		}
		return codegraphDerefString(scanned.RepoID)
	default:
		return ""
	}
}

func repoIDFromFunctionID(functionID string) string {
	if idx := strings.Index(functionID, "\x1f"); idx >= 0 {
		return strings.TrimSpace(functionID[:idx])
	}
	return ""
}
