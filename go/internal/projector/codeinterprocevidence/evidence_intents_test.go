// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinterprocevidence

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildCodeInterprocEvidenceReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: "file"}})
	if _, ok := BuildCodeInterprocEvidenceReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued an interproc intent without any code_interproc_evidence fact")
	}
}

func TestBuildCodeInterprocEvidenceReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildCodeInterprocEvidenceReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued an interproc intent for a generation with no facts at all")
	}
}

func TestBuildCodeInterprocEvidenceReducerIntentFromFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "interproc-fact-1", CollectorKind: "git"},
	})
	intent, ok := BuildCodeInterprocEvidenceReducerIntent("scope-1", "gen-1", lookup)
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

// TestBuildCodeInterprocEvidenceReducerIntentSkipsFunctionSummaryFact pins the
// boundary with the neighboring family: a code_function_summary fact belongs to
// the summary-persistence domain and must not trigger a direct interproc
// intent — summary-driven fixpoint projection runs only after the
// function-summary handler persists its durable stores.
func TestBuildCodeInterprocEvidenceReducerIntentSkipsFunctionSummaryFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeFunctionSummaryFactKind, FactID: "summary-fact-1", CollectorKind: "git"},
	})
	if _, ok := BuildCodeInterprocEvidenceReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued direct interproc intent for code_function_summary fact")
	}
}

// TestBuildCodeInterprocEvidenceReducerIntentFromMarkerOnly proves the dataflow
// marker alone (no findings) queues a retraction intent so stale TAINT_FLOWS_TO
// edges from a prior generation are cleared when the current finding set is
// empty (#2919).
func TestBuildCodeInterprocEvidenceReducerIntentFromMarkerOnly(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeDataflowScannedFactKind, FactID: "marker-1", CollectorKind: "git"},
	})
	intent, ok := BuildCodeInterprocEvidenceReducerIntent("scope-1", "gen-1", lookup)
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

// TestBuildCodeInterprocEvidenceReducerIntentPrefersFindingProvenance pins the
// documented rule that an interproc finding outranks the dataflow marker even
// when the marker appears earlier in the generation's original input order —
// the two kinds are looked up independently, not merged by position.
func TestBuildCodeInterprocEvidenceReducerIntentPrefersFindingProvenance(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.CodeDataflowScannedFactKind, FactID: "marker-1", CollectorKind: "git"},
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "finding-1", CollectorKind: "git"},
	})
	intent, ok := BuildCodeInterprocEvidenceReducerIntent("scope-1", "gen-1", lookup)
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

// TestBuildCodeInterprocEvidenceReducerIntentTrimsCollectorKind pins the
// single-tier source-system label: the trimmed CollectorKind alone, never the
// two-tier projectorintent.SourceSystem fallback. A trigger fact carrying a
// SourceRef identity must still label the intent with its collector kind.
func TestBuildCodeInterprocEvidenceReducerIntentTrimsCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.CodeInterprocEvidenceFactKind,
		FactID:        "interproc-fact-1",
		CollectorKind: "  git  ",
		SourceRef:     facts.Ref{SourceSystem: "source-ref-system"},
	}})
	intent, ok := BuildCodeInterprocEvidenceReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_interproc_evidence fact")
	}
	if intent.SourceSystem != "git" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind, not the SourceRef identity", intent.SourceSystem)
	}
}
