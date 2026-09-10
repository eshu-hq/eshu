// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: "file"}})
	if _, ok := BuildReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued an interproc intent without any code_interproc_evidence fact")
	}
}

func TestBuildReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued an interproc intent for a generation with no facts at all")
	}
}

func TestBuildReducerIntentFromFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "interproc-fact-1", CollectorKind: "git"},
	})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_interproc_evidence fact")
	}
	if intent.Domain != reducer.DomainCodeInterprocEvidence {
		t.Fatalf("intent.Domain = %q, want code_interproc_evidence", intent.Domain)
	}
	if intent.EntityKey != "code_interproc_evidence:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.Reason != "cross-function value-flow evidence observed" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "interproc-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
}

// TestBuildReducerIntentSkipsFunctionSummaryFact verifies that summary facts
// remain owned by the summary-persistence domain.
func TestBuildReducerIntentSkipsFunctionSummaryFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeFunctionSummaryFactKind, FactID: "summary-fact-1", CollectorKind: "git"},
	})
	if _, ok := BuildReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued direct interproc intent for code_function_summary fact")
	}
}

// TestBuildReducerIntentFromMarkerOnly verifies the #2919 stale-edge
// retraction trigger for scans with no findings.
func TestBuildReducerIntentFromMarkerOnly(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeDataflowScannedFactKind, FactID: "marker-1", CollectorKind: "git"},
	})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a dataflow marker without findings")
	}
	if intent.Domain != reducer.DomainCodeInterprocEvidence || intent.EntityKey != "code_interproc_evidence:scope-1" {
		t.Fatalf("intent domain/key wrong: %+v", intent)
	}
	if intent.Reason != "value-flow gate scanned; reconcile cross-function evidence" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "marker-1" || intent.SourceSystem != "git" {
		t.Fatalf("marker provenance not carried: %+v", intent)
	}
}

// TestBuildReducerIntentPrefersFindingProvenance verifies that a finding wins
// provenance regardless of cross-kind input order.
func TestBuildReducerIntentPrefersFindingProvenance(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.CodeDataflowScannedFactKind, FactID: "marker-1", CollectorKind: "git"},
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "finding-1", CollectorKind: "git"},
	})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued when both a finding and the marker are present")
	}
	if intent.FactID != "finding-1" {
		t.Fatalf("intent.FactID = %q, want the finding finding-1 to outrank the earlier marker", intent.FactID)
	}
	if intent.Reason != "cross-function value-flow evidence observed" {
		t.Fatalf("intent.Reason = %q, want the finding reason", intent.Reason)
	}
}

// TestBuildReducerIntentTrimsCollectorKind verifies the single-tier source
// label even when the trigger also carries a SourceRef identity.
func TestBuildReducerIntentTrimsCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.CodeInterprocEvidenceFactKind,
		FactID:        "interproc-fact-1",
		CollectorKind: "  git  ",
		SourceRef:     facts.Ref{SourceSystem: "source-ref-system"},
	}})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_interproc_evidence fact")
	}
	if intent.SourceSystem != "git" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind, not the SourceRef identity", intent.SourceSystem)
	}
}
