// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
)

// TestBuildReducerIntentPrefersSummaryProvenanceOverEarlierMarker verifies
// that a summary finding wins provenance regardless of cross-kind input order.
func TestBuildReducerIntentPrefersSummaryProvenanceOverEarlierMarker(t *testing.T) {
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
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
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

// TestBuildReducerIntentFallsBackToMarkerRepoIDWhenSummaryRepoIDUnresolvable
// verifies marker repo-ID fallback without changing summary provenance.
func TestBuildReducerIntentFallsBackToMarkerRepoIDWhenSummaryRepoIDUnresolvable(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{
			FactKind:      facts.CodeFunctionSummaryFactKind,
			FactID:        "summary-fact-1",
			CollectorKind: "git",
			// No function_id key: decodeFunctionSummary fails, so the
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
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
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

// TestBuildReducerIntentTrimsCollectorKind verifies the single-tier source
// label even when the trigger also carries a SourceRef identity.
func TestBuildReducerIntentTrimsCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.CodeFunctionSummaryFactKind,
		FactID:        "summary-fact-1",
		CollectorKind: "  git  ",
		SourceRef:     facts.Ref{SourceSystem: "source-ref-system"},
		Payload:       map[string]any{"function_id": "repo-1\x1fpkg\x1f\x1fHandle"},
	}})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_function_summary fact")
	}
	if intent.SourceSystem != "git" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind, not the SourceRef identity", intent.SourceSystem)
	}
}
