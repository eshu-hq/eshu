// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// These tests stay at root after the builder moved into
// internal/projector/codetaintevidence because they assert root's dispatcher
// wiring through buildProjection, not the builder in isolation — the builder's
// focused cases live in the child package.

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
