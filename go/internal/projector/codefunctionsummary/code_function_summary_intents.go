// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codefunctionsummary

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildCodeFunctionSummaryReducerIntent queues one function-summary persistence
// intent per scope generation when either summary facts or the full-snapshot
// value-flow scan marker are present. Summary facts refresh changed functions;
// the full marker additionally lets the reducer replace the repo snapshot and
// prune summaries deleted or renamed out of the latest complete scan.
func BuildCodeFunctionSummaryReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	// The two candidate kinds are looked up independently, not merged in
	// original order: a summary fact always outranks the marker as the
	// trigger regardless of which appears earlier in the generation, and the
	// payload construction below needs both facts' repo IDs, not just the
	// winning trigger's.
	summaryFact, hasSummaryFact := lookup.FirstOfKind(facts.CodeFunctionSummaryFactKind)
	markerFact, hasMarkerFact := lookup.FirstOfKind(facts.CodeDataflowScannedFactKind)

	var trigger *facts.Envelope
	reason := "value-flow function summaries observed"
	if hasSummaryFact {
		trigger = &summaryFact
	} else if hasMarkerFact {
		trigger = &markerFact
		reason = "value-flow gate scanned; reconcile function summaries"
	}
	if trigger == nil {
		return projectorintent.ReducerIntent{}, false
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
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainCodeFunctionSummary,
		EntityKey:    "code_function_summary:" + scopeID,
		Reason:       reason,
		FactID:       trigger.FactID,
		SourceSystem: strings.TrimSpace(trigger.CollectorKind),
		Payload:      payload,
	}, true
}

// codeFunctionSummaryTriggerRepoID resolves the repo id a trigger fact carries,
// decoding a code_function_summary fact's function_id prefix or a
// code_dataflow_scanned marker's repo_id field. It returns "" on a nil
// trigger, an unrecognized fact kind, or a decode failure — the caller treats
// an empty repo id as "omit repo_id from the payload", never as a reason to
// drop the intent itself.
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
		return derefString(scanned.RepoID)
	default:
		return ""
	}
}

// repoIDFromFunctionID extracts the repo id prefix from a function id encoded
// as "<repo_id>\x1f<package>\x1f<receiver>\x1f<name>". It returns "" when the
// unit separator is absent, which the caller treats as no resolvable repo id
// rather than a decode error.
func repoIDFromFunctionID(functionID string) string {
	if idx := strings.Index(functionID, "\x1f"); idx >= 0 {
		return strings.TrimSpace(functionID[:idx])
	}
	return ""
}

// derefString returns the value a *string points at, or "" when it is nil.
// Local per-package copy matching the repo convention of a small
// family-scoped deref helper (e.g. projector root's codegraphDerefString,
// ec2's derefString, s3's derefString) rather than a shared one.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
