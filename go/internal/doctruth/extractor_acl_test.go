// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractorPropagatesBoundedSourceACLStateOntoEvidence proves a bounded
// source_acl_state observed on the section's document is carried verbatim onto
// the derived mention and claim evidence payloads. The extractor never upgrades
// a denied, partial, missing, or stale observation to allowed (#2178).
func TestExtractorPropagatesBoundedSourceACLStateOntoEvidence(t *testing.T) {
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

			extractor := doctruth.NewExtractor([]doctruth.Entity{
				{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
			}, doctruth.Options{})
			section := baseSectionInput("payment-api owns customer payment authorization.")
			section.SourceACLState = state
			section.ClaimHints = []doctruth.ClaimHint{{
				ClaimID:     "claim:payment-api:auth",
				ClaimType:   "service_capability",
				ClaimText:   "payment-api owns customer payment authorization.",
				SubjectText: "payment-api",
				SubjectKind: "service",
			}}

			result, err := extractor.Extract(context.Background(), section)
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}

			mention := onlyPayload[facts.DocumentationEntityMentionPayload](t, result.Envelopes, facts.DocumentationEntityMentionFactKind)
			if mention.ACLSummary == nil {
				t.Fatalf("mention ACLSummary = nil, want source_acl_state %q", state)
			}
			if got := mention.ACLSummary.SourceACLState; got != state {
				t.Fatalf("mention source_acl_state = %q, want %q (verbatim)", got, state)
			}

			claim := onlyPayload[facts.DocumentationClaimCandidatePayload](t, result.Envelopes, facts.DocumentationClaimCandidateFactKind)
			if claim.ACLSummary == nil {
				t.Fatalf("claim ACLSummary = nil, want source_acl_state %q", state)
			}
			if got := claim.ACLSummary.SourceACLState; got != state {
				t.Fatalf("claim source_acl_state = %q, want %q (verbatim)", got, state)
			}
		})
	}
}

// TestExtractorOmitsSourceACLStateWhenUnobserved proves the extractor omits the
// acl_summary entirely when the document asserted no bounded ACL claim. An empty
// or non-bounded value is never defaulted to a guessed posture (#2178).
func TestExtractorOmitsSourceACLStateWhenUnobserved(t *testing.T) {
	t.Parallel()

	for name, state := range map[string]string{
		"empty":       "",
		"non-bounded": "unknown",
	} {
		state := state
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			extractor := doctruth.NewExtractor([]doctruth.Entity{
				{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
			}, doctruth.Options{})
			section := baseSectionInput("payment-api owns customer payment authorization.")
			section.SourceACLState = state

			result, err := extractor.Extract(context.Background(), section)
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}

			mention := onlyPayload[facts.DocumentationEntityMentionPayload](t, result.Envelopes, facts.DocumentationEntityMentionFactKind)
			if mention.ACLSummary != nil {
				t.Fatalf("mention ACLSummary = %#v, want nil for unobserved state %q", mention.ACLSummary, state)
			}
		})
	}
}
