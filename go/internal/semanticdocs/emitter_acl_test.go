// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticdocs

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestEmitterPropagatesBoundedSourceACLStateOntoObservation proves the bounded
// source_acl_state observed on the section's document is carried verbatim onto
// the semantic documentation observation payload. The emitter never upgrades a
// denied, partial, missing, or stale observation to allowed (#2178).
func TestEmitterPropagatesBoundedSourceACLStateOntoObservation(t *testing.T) {
	t.Parallel()

	for _, state := range []string{
		facts.SourceACLStateDenied,
		facts.SourceACLStatePartial,
		facts.SourceACLStateMissing,
		facts.SourceACLStateStale,
	} {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			emitter := mustACLEmitter(t)
			section := semanticSectionFixture()
			section.SourceACLState = state

			payload := emitOneObservationPayload(t, emitter, section)
			if payload.ACLSummary == nil {
				t.Fatalf("observation ACLSummary = nil, want source_acl_state %q", state)
			}
			if got := payload.ACLSummary.SourceACLState; got != state {
				t.Fatalf("observation source_acl_state = %q, want %q (verbatim)", got, state)
			}
		})
	}
}

// TestEmitterOmitsSourceACLStateWhenUnobserved proves the emitter omits the
// acl_summary entirely when the document asserted no bounded ACL claim. An empty
// or non-bounded value is never defaulted to a guessed posture (#2178).
func TestEmitterOmitsSourceACLStateWhenUnobserved(t *testing.T) {
	t.Parallel()

	for name, state := range map[string]string{
		"empty":       "",
		"non-bounded": "unknown",
	} {
		state := state
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			emitter := mustACLEmitter(t)
			section := semanticSectionFixture()
			section.SourceACLState = state

			payload := emitOneObservationPayload(t, emitter, section)
			if payload.ACLSummary != nil {
				t.Fatalf("observation ACLSummary = %#v, want nil for unobserved state %q", payload.ACLSummary, state)
			}
		})
	}
}

func mustACLEmitter(t *testing.T) *Emitter {
	t.Helper()

	emitter, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-test",
			ProviderKind:      ProviderKindMock,
		},
		ObservedAt: fixedObservationTime,
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v, want nil", err)
	}
	return emitter
}

func emitOneObservationPayload(t *testing.T, emitter *Emitter, section doctruth.SectionInput) facts.SemanticDocumentationObservationPayload {
	t.Helper()

	payload, err := emitter.payload(section, MockObservation{
		ObservationType: "summary",
		ObservationText: "Deploy the payment service through the production Helm release.",
		AdmissionState:  facts.SemanticAdmissionDocumentationFindingCandidate,
	}, 0)
	if err != nil {
		t.Fatalf("payload() error = %v, want nil", err)
	}
	return payload
}
