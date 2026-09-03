// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codefunctionsummary

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
)

// TestBuildCodeFunctionSummaryReducerIntentPrefersSummaryProvenanceOverEarlierMarker
// pins that a code_function_summary fact outranks the code_dataflow_scanned
// marker as the intent's FactID/Reason provenance even when the marker
// appears earlier in the generation's original input order — the two kinds
// are looked up independently via FirstOfKind, not merged by position. Before
// this test, no case in this package proved order-independence: the moved
// TestBuildCodeFunctionSummaryReducerIntentFromFact case never placed a
// marker fact ahead of the summary fact, so a regression to
// FirstAcrossKinds-style positional merging would not have failed any test.
func TestBuildCodeFunctionSummaryReducerIntentPrefersSummaryProvenanceOverEarlierMarker(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{
			FactKind:      facts.CodeDataflowScannedFactKind,
			FactID:        "marker-1",
			CollectorKind: "git",
			Payload:       map[string]any{"repo_id": "repo-marker"},
		},
		{
			FactKind:      facts.CodeFunctionSummaryFactKind,
			FactID:        "summary-fact-1",
			CollectorKind: "git",
			Payload:       map[string]any{"function_id": "repo-summary\x1fpkg\x1f\x1fHandle"},
		},
	})
	intent, ok := BuildCodeFunctionSummaryReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued when both a summary fact and the marker are present")
	}
	if intent.FactID != "summary-fact-1" {
		t.Fatalf("intent.FactID = %q, want the summary fact summary-fact-1 to outrank the earlier marker", intent.FactID)
	}
	if intent.Reason != "value-flow function summaries observed" {
		t.Fatalf("intent.Reason = %q, want the summary reason", intent.Reason)
	}
	if intent.Payload["repo_id"] != "repo-summary" {
		t.Fatalf("intent.Payload[repo_id] = %v, want the summary fact's own repo id", intent.Payload["repo_id"])
	}
	if intent.Payload["full_snapshot"] != true {
		t.Fatalf("intent.Payload = %#v, want full_snapshot true whenever the marker is also present", intent.Payload)
	}
}

// TestBuildCodeFunctionSummaryReducerIntentFallsBackToMarkerRepoIDWhenSummaryRepoIDUnresolvable
// pins the fallback branch in codeFunctionSummaryTriggerRepoID: when both
// facts are present and the summary fact wins provenance but its own
// function_id does not decode to a repo id, the payload borrows the marker's
// repo_id instead of omitting it — while FactID and Reason stay bound to the
// summary trigger. The pre-extraction test suite covered "summary present,
// no marker, unresolvable repo id" (repo_id omitted) and "marker only" (repo_id
// from the marker) separately, but never the case where both facts are
// present AND the summary's own repo id is unresolvable at the same time —
// exactly the branch this test exercises. Deleting the fallback's
// `hasMarkerFact && hasSummaryFact` guard collapses to always-omit and passes
// every other test in this package silently.
func TestBuildCodeFunctionSummaryReducerIntentFallsBackToMarkerRepoIDWhenSummaryRepoIDUnresolvable(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{
			FactKind:      facts.CodeFunctionSummaryFactKind,
			FactID:        "summary-fact-1",
			CollectorKind: "git",
			// No function_id key: decodeCodeFunctionSummary fails, so the
			// summary's own repo id resolves to "".
			Payload: map[string]any{"repo_id": "ignored-not-a-function-summary-field"},
		},
		{
			FactKind:      facts.CodeDataflowScannedFactKind,
			FactID:        "marker-1",
			CollectorKind: "git",
			Payload:       map[string]any{"repo_id": "repo-from-marker"},
		},
	})
	intent, ok := BuildCodeFunctionSummaryReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued when both facts are present")
	}
	if intent.FactID != "summary-fact-1" || intent.Reason != "value-flow function summaries observed" {
		t.Fatalf("intent trigger = %+v, want summary provenance despite the repo-id fallback", intent)
	}
	if intent.Payload["repo_id"] != "repo-from-marker" {
		t.Fatalf("intent.Payload[repo_id] = %v, want the marker's repo id as fallback", intent.Payload["repo_id"])
	}
	if intent.Payload["full_snapshot"] != true {
		t.Fatalf("intent.Payload = %#v, want full_snapshot true", intent.Payload)
	}
}

// TestBuildCodeFunctionSummaryReducerIntentTrimsCollectorKind pins the
// single-tier source-system label: the trimmed CollectorKind alone, never the
// two-tier projectorintent.SourceSystem fallback that would prefer a
// SourceRef identity when one is set. This family's pre-extraction tests
// never varied CollectorKind or SourceRef, so a substitution of the two-tier
// helper would not have failed any test before this one.
func TestBuildCodeFunctionSummaryReducerIntentTrimsCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.CodeFunctionSummaryFactKind,
		FactID:        "summary-fact-1",
		CollectorKind: "  git  ",
		SourceRef:     facts.Ref{SourceSystem: "source-ref-system"},
		Payload:       map[string]any{"function_id": "repo-1\x1fpkg\x1f\x1fHandle"},
	}})
	intent, ok := BuildCodeFunctionSummaryReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_function_summary fact")
	}
	if intent.SourceSystem != "git" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind, not the SourceRef identity", intent.SourceSystem)
	}
}
