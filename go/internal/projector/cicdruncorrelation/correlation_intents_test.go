// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdruncorrelation

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildCICDRunCorrelationReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: "file"}})
	if _, ok := BuildCICDRunCorrelationReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a ci_cd_run_correlation intent without any ci.run or ci.artifact fact")
	}
}

func TestBuildCICDRunCorrelationReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildCICDRunCorrelationReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a ci_cd_run_correlation intent for a generation with no facts at all")
	}
}

func TestBuildCICDRunCorrelationReducerIntentFromRunFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file"},
		{
			FactKind:      facts.CICDRunFactKind,
			FactID:        "run-fact-1",
			CollectorKind: "github_actions",
			SourceRef:     facts.Ref{SourceSystem: "github_actions"},
		},
	})
	intent, ok := BuildCICDRunCorrelationReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a ci.run fact")
	}
	if intent.Domain != reducer.DomainCICDRunCorrelation {
		t.Fatalf("intent.Domain = %q, want ci_cd_run_correlation", intent.Domain)
	}
	if intent.EntityKey != "ci_cd_run_correlation:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.Reason != "ci/cd run-scoped evidence observed" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "run-fact-1" {
		t.Fatalf("intent.FactID = %q, want the ci.run fact", intent.FactID)
	}
	if intent.SourceSystem != "github_actions" {
		t.Fatalf("intent.SourceSystem = %q, want %q", intent.SourceSystem, "github_actions")
	}
}

// TestBuildCICDRunCorrelationReducerIntentFromArtifactOnly proves an
// artifact-only generation (no co-located ci.run) still triggers the
// correlation intent so the reducer can run its bounded historical-run patch
// (#5770).
func TestBuildCICDRunCorrelationReducerIntentFromArtifactOnly(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.CICDArtifactFactKind, FactID: "artifact-fact-1", CollectorKind: "github_actions"},
	})
	intent, ok := BuildCICDRunCorrelationReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for an artifact-only generation")
	}
	if intent.FactID != "artifact-fact-1" {
		t.Fatalf("intent.FactID = %q, want the ci.artifact fact", intent.FactID)
	}
}

// TestBuildCICDRunCorrelationReducerIntentPrefersRunOverArtifact pins the
// documented anchor rule: when both ci.run and ci.artifact are present in the
// same generation, the run is the anchor even when the artifact appears
// earlier in the generation's original input order.
func TestBuildCICDRunCorrelationReducerIntentPrefersRunOverArtifact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.CICDArtifactFactKind, FactID: "artifact-fact-2", CollectorKind: "github_actions"},
		{FactKind: facts.CICDRunFactKind, FactID: "run-fact-2", CollectorKind: "github_actions"},
	})
	intent, ok := BuildCICDRunCorrelationReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued when both a run and an artifact are present")
	}
	if intent.FactID != "run-fact-2" {
		t.Fatalf("intent.FactID = %q, want the run fact run-fact-2 (the anchor), even though the artifact fact appears first in input order", intent.FactID)
	}
}

// TestBuildCICDRunCorrelationReducerIntentSourceSystemFallsBackToCollectorKind
// pins the two-tier projectorintent.SourceSystem label this family uses
// verbatim: SourceRef.SourceSystem wins when set, else the trimmed
// CollectorKind.
func TestBuildCICDRunCorrelationReducerIntentSourceSystemFallsBackToCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind:      facts.CICDRunFactKind,
		FactID:        "run-fact-3",
		CollectorKind: "  github_actions  ",
	}})
	intent, ok := BuildCICDRunCorrelationReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for a ci.run fact")
	}
	if intent.SourceSystem != "github_actions" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind fallback", intent.SourceSystem)
	}
}
