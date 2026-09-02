// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// This test stays at root after the builder moved into
// internal/projector/codeinterprocevidence because it asserts root's dispatcher
// wiring through appendScopeGenerationReducerIntents, not the builder in
// isolation — the builder's focused cases live in the child package. The
// buildProjection marker case proving BOTH value-flow retraction domains
// enqueue (#2919) lives in code_taint_evidence_projection_test.go.

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
