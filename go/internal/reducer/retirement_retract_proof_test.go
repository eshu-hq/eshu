// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

// This file is the reducer half of the issue #1800 retirement proof lane. It
// proves the contract that protects reducer-owned graph truth when a later
// generation supersedes an earlier one:
//
//   - When a scope has a prior generation, the handler retracts the prior
//     reducer-owned edges BEFORE writing the current generation's edges, so a
//     CAN_PERFORM edge that the new evidence no longer supports cannot survive.
//   - The retract is scoped to the reducer's own evidence source, never edges
//     owned by other writers.
//   - On the very first generation there is no prior edge set, so no retract
//     fires (proven by TestIAMCanPerformHandlerSkipsFirstGenerationRetract).
//
// The supersession of the underlying generation pointer is proven in
// internal/storage/postgres (proof_domain_retirement_test.go and
// projector_queue_lifecycle_test.go); this test proves the reducer reacts to
// that supersession by replacing, not accumulating, its edges.

// orderedIAMCanPerformWriter records write and retract calls in invocation
// order so the test can assert retract happens before the rewrite.
type orderedIAMCanPerformWriter struct {
	events         []string
	retractScopes  []string
	retractSource  string
	writeSource    string
	writtenForGens []string
}

func (w *orderedIAMCanPerformWriter) WriteIAMCanPerformEdges(
	_ context.Context, _ []map[string]any, _, generationID, evidenceSource string,
) error {
	w.events = append(w.events, "write")
	w.writeSource = evidenceSource
	w.writtenForGens = append(w.writtenForGens, generationID)
	return nil
}

func (w *orderedIAMCanPerformWriter) RetractIAMCanPerformEdges(
	_ context.Context, scopeIDs []string, _, evidenceSource string,
) error {
	w.events = append(w.events, "retract")
	w.retractScopes = append(w.retractScopes, scopeIDs...)
	w.retractSource = evidenceSource
	return nil
}

// TestProofRetirementReducerRetractsPriorGenerationEdgesBeforeRewrite proves
// that when a later generation becomes active for a scope (PriorGenerationCheck
// reports a prior generation exists), the reducer retracts its prior edges
// scoped to its own evidence source before writing the current generation's
// edges. This is the mechanism that keeps a stale CAN_PERFORM edge from
// surviving after the evidence that justified it was removed in a refresh.
func TestProofRetirementReducerRetractsPriorGenerationEdgesBeforeRewrite(t *testing.T) {
	t.Parallel()

	writer := &orderedIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanPerformFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), iamCanPerformIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if len(writer.events) != 2 {
		t.Fatalf("writer events = %v, want [retract write]", writer.events)
	}
	if writer.events[0] != "retract" || writer.events[1] != "write" {
		t.Fatalf("writer event order = %v, want retract before write", writer.events)
	}
	// Retract and write must target the reducer's own evidence source so the
	// delete never touches edges owned by another writer.
	if writer.retractSource != iamCanPerformEvidenceSource {
		t.Fatalf("retract evidence source = %q, want %q", writer.retractSource, iamCanPerformEvidenceSource)
	}
	if writer.writeSource != iamCanPerformEvidenceSource {
		t.Fatalf("write evidence source = %q, want %q", writer.writeSource, iamCanPerformEvidenceSource)
	}
	if len(writer.retractScopes) != 1 || writer.retractScopes[0] != "scope-1" {
		t.Fatalf("retract scopes = %v, want [scope-1]", writer.retractScopes)
	}
}

// TestProofRetirementReducerRetractsOnRetryEvenOnFirstGeneration proves the
// safety boundary for the first generation under retry: the first-generation
// retract skip only applies on the first attempt. A retried first-generation
// attempt still retracts so a partial prior write from the failed attempt is
// cleaned up before the rewrite. This keeps the first-generation optimization
// from leaving half-written edges behind.
func TestProofRetirementReducerRetractsOnRetryEvenOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &orderedIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanPerformFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	intent := iamCanPerformIntent()
	intent.AttemptCount = 2 // a retried attempt, even on the first generation

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if len(writer.events) != 2 || writer.events[0] != "retract" {
		t.Fatalf("retried first-generation events = %v, want retract before write", writer.events)
	}
}
