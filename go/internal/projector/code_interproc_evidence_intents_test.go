// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCodeInterprocEvidenceReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	if _, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{{FactKind: "file"}})); ok {
		t.Fatal("queued an interproc intent without any code_interproc_evidence fact")
	}
}

func TestBuildCodeInterprocEvidenceReducerIntentFromFact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "interproc-fact-1", CollectorKind: "git"},
	}))
	if !ok {
		t.Fatal("no intent queued for a code_interproc_evidence fact")
	}
	if intent.Domain != reducer.DomainCodeInterprocEvidence {
		t.Fatalf("intent.Domain = %q, want code_interproc_evidence", intent.Domain)
	}
	if intent.EntityKey != "code_interproc_evidence:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.FactID != "interproc-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
}

func TestBuildCodeInterprocEvidenceReducerIntentSkipsFunctionSummaryFact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	if _, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeFunctionSummaryFactKind, FactID: "summary-fact-1", CollectorKind: "git"},
	})); ok {
		t.Fatal("queued direct interproc intent for code_function_summary fact")
	}
}

// TestBuildCodeInterprocEvidenceReducerIntentFromMarkerOnly proves the dataflow
// marker alone (no findings) queues a retraction intent so stale edges from a
// prior generation are cleared when the current finding set is empty (#2919).
func TestBuildCodeInterprocEvidenceReducerIntentFromMarkerOnly(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeDataflowScannedFactKind, FactID: "marker-1", CollectorKind: "git"},
	}))
	if !ok {
		t.Fatal("no intent queued for a dataflow marker without findings")
	}
	if intent.Domain != reducer.DomainCodeInterprocEvidence || intent.EntityKey != "code_interproc_evidence:scope-1" {
		t.Fatalf("intent domain/key wrong: %+v", intent)
	}
	if intent.FactID != "marker-1" || intent.SourceSystem != "git" {
		t.Fatalf("marker provenance not carried: %+v", intent)
	}
}

// TestBuildCodeInterprocEvidenceReducerIntentPrefersFindingProvenance proves a
// finding is preferred over the marker as the intent's provenance when both are
// present.
func TestBuildCodeInterprocEvidenceReducerIntentPrefersFindingProvenance(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{
		{FactKind: facts.CodeDataflowScannedFactKind, FactID: "marker-1", CollectorKind: "git"},
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "finding-1", CollectorKind: "git"},
	}))
	if !ok || intent.FactID != "finding-1" {
		t.Fatalf("finding provenance not preferred over marker: %+v", intent)
	}
}

// TestAppendScopeGenerationReducerIntentsWiresCodeInterproc proves the interproc
// builder is actually wired into the scope-generation intent chain, not just
// defined in isolation.
func TestAppendScopeGenerationReducerIntentsWiresCodeInterproc(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intents := appendScopeGenerationReducerIntents(nil, scopeValue, generation, []facts.Envelope{
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "interproc-fact-1", CollectorKind: "git"},
	})
	found := false
	for _, intent := range intents {
		if intent.Domain == reducer.DomainCodeInterprocEvidence {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("code_interproc_evidence intent not produced by the scope-generation chain")
	}
}
