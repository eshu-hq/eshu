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
		t.Fatal("queued a taint intent without any code_taint_evidence fact")
	}
}

func TestBuildReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a taint intent for a generation with no facts at all")
	}
}

func TestBuildReducerIntentFromFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeTaintEvidenceFactKind, FactID: "taint-fact-1", CollectorKind: "git"},
	})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_taint_evidence fact")
	}
	if intent.Domain != reducer.DomainCodeTaintEvidence {
		t.Fatalf("intent.Domain = %q, want code_taint_evidence", intent.Domain)
	}
	if intent.EntityKey != "code_taint_evidence:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.Reason != "value-flow taint evidence observed" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "taint-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
}

// TestBuildReducerIntentFromMarkerOnly verifies the #2919 stale-evidence
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
	if intent.Domain != reducer.DomainCodeTaintEvidence || intent.EntityKey != "code_taint_evidence:scope-1" {
		t.Fatalf("intent domain/key wrong: %+v", intent)
	}
	if intent.Reason != "value-flow gate scanned; reconcile taint evidence" {
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
		{FactKind: facts.CodeTaintEvidenceFactKind, FactID: "taint-fact-1", CollectorKind: "git"},
	})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued when both a finding and the marker are present")
	}
	if intent.FactID != "taint-fact-1" {
		t.Fatalf("intent.FactID = %q, want the finding taint-fact-1 to outrank the earlier marker", intent.FactID)
	}
	if intent.Reason != "value-flow taint evidence observed" {
		t.Fatalf("intent.Reason = %q, want the finding reason", intent.Reason)
	}
}

// TestBuildReducerIntentTrimsCollectorKind verifies the single-tier source
// label even when the trigger also carries a SourceRef identity.
func TestBuildReducerIntentTrimsCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.CodeTaintEvidenceFactKind,
		FactID:        "taint-fact-1",
		CollectorKind: "  git  ",
		SourceRef:     facts.Ref{SourceSystem: "source-ref-system"},
	}})
	intent, ok := BuildReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a code_taint_evidence fact")
	}
	if intent.SourceSystem != "git" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind, not the SourceRef identity", intent.SourceSystem)
	}
}
