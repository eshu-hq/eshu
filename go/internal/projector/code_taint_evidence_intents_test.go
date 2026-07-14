// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCodeTaintEvidenceReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	if _, ok := buildCodeTaintEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{{FactKind: "file"}})); ok {
		t.Fatal("queued a taint intent without any code_taint_evidence fact")
	}
}

func TestBuildCodeTaintEvidenceReducerIntentFromFact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeTaintEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeTaintEvidenceFactKind, FactID: "taint-fact-1", CollectorKind: "git"},
	}))
	if !ok {
		t.Fatal("no intent queued for a code_taint_evidence fact")
	}
	if intent.Domain != reducer.DomainCodeTaintEvidence {
		t.Fatalf("intent.Domain = %q, want code_taint_evidence", intent.Domain)
	}
	if intent.EntityKey != "code_taint_evidence:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.FactID != "taint-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
}

// TestBuildCodeTaintEvidenceReducerIntentFromMarkerOnly proves the dataflow marker
// alone (no findings) queues a retraction intent so stale CodeTaintEvidence from a
// prior generation is cleared when the current finding set is empty (#2919).
func TestBuildCodeTaintEvidenceReducerIntentFromMarkerOnly(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeTaintEvidenceReducerIntent(scopeValue, generation, newReducerIntentFactIndex([]facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeDataflowScannedFactKind, FactID: "marker-1", CollectorKind: "git"},
	}))
	if !ok {
		t.Fatal("no intent queued for a dataflow marker without findings")
	}
	if intent.Domain != reducer.DomainCodeTaintEvidence || intent.EntityKey != "code_taint_evidence:scope-1" {
		t.Fatalf("intent domain/key wrong: %+v", intent)
	}
	if intent.FactID != "marker-1" || intent.SourceSystem != "git" {
		t.Fatalf("marker provenance not carried: %+v", intent)
	}
}

// TestBuildProjectionQueuesBothEvidenceDomainsFromMarker proves the live runtime
// projection enqueues BOTH value-flow evidence retraction intents from the
// dataflow marker alone — the empty-generation reconciliation path (#2919).
func TestBuildProjectionQueuesBothEvidenceDomainsFromMarker(t *testing.T) {
	t.Parallel()

	scopeValue, generation := incidentRoutingProjectionScope()
	envelopes := []facts.Envelope{{
		FactKind:      facts.CodeDataflowScannedFactKind,
		FactID:        "marker-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		CollectorKind: "git",
		Payload:       map[string]any{"reason": "value-flow gate scanned the repository snapshot"},
	}}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v", err)
	}
	taint := intentForDomain(t, projection.reducerIntents, reducer.DomainCodeTaintEvidence)
	if taint.FactID != "marker-1" {
		t.Fatalf("taint intent.FactID = %q, want marker-1", taint.FactID)
	}
	interproc := intentForDomain(t, projection.reducerIntents, reducer.DomainCodeInterprocEvidence)
	if interproc.FactID != "marker-1" {
		t.Fatalf("interproc intent.FactID = %q, want marker-1", interproc.FactID)
	}
}

// TestBuildProjectionQueuesCodeTaintEvidence proves the live runtime projection
// (buildProjection -> appendScopeGenerationReducerIntents) enqueues a
// DomainCodeTaintEvidence intent from a code_taint_evidence fact. This is the
// same FactKind-based intent path the incident-routing domain uses; the fact
// carries graph_kind only (no reducer_domain), so the scope-generation builder —
// not the payload-domain buildReducerIntent — is what enqueues it.
func TestBuildProjectionQueuesCodeTaintEvidence(t *testing.T) {
	t.Parallel()

	scopeValue, generation := incidentRoutingProjectionScope()
	envelopes := []facts.Envelope{{
		FactKind:      facts.CodeTaintEvidenceFactKind,
		FactID:        "taint-fact-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		CollectorKind: "git",
		Payload:       map[string]any{"graph_kind": "code_taint_evidence", "function_uid": "func-1"},
	}}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainCodeTaintEvidence)
	if intent.FactID != "taint-fact-1" {
		t.Fatalf("intent.FactID = %q, want taint-fact-1", intent.FactID)
	}
	if intent.EntityKey != "code_taint_evidence:"+scopeValue.ScopeID {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
}
